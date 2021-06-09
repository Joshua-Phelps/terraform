package globalref

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/lang"
)

// MetaReferences inspects the configuration to find the references contained
// within the most specific object that the given address refers to.
//
// This finds only the direct references in that object, not any indirect
// references from those. This is a building block for some other Analyzer
// functions that can walk through multiple levels of reference.
//
// MetaReferences also returns the address of the module that all of the
// resulting references are relative to. For most reference types that'll just
// be the same as the given moduleAddr, but input variables and output values
// both cross module boundaries and so will return a different module address.
// When passing an input variable or output value address, always pass the
// module address where the given reference was found: the callee for an
// input variable, and the caller for an output value.
//
// References are always local to a particular module. However, beware a
// special situation for input variables and output values, because they
// both cross over the boundary between modules. Always pass the address
// of the module where "ref" came from when calling MetaReferences, but
// then interpret the result in the other module. That is, for input
// variables you must interpret the result in the caller, while for output
// values you must interpret the result in the callee.
//
// If the given reference refers to something that doesn't exist in the
// configuration we're analyzing then MetaReferences will return no
// meta-references at all, which is indistinguishable from an existing
// object that doesn't refer to anything.
func (a *Analyzer) MetaReferences(moduleAddr addrs.ModuleInstance, ref *addrs.Reference) (addrs.ModuleInstance, []*addrs.Reference) {
	// This function is aiming to encapsulate the fact that a reference
	// is actually quite a complex notion which includes both a specific
	// object the reference is to, where each distinct object type has
	// a very different representation in the configuration, and then
	// also potentially an attribute or block within the definition of that
	// object. Our goal is to make all of these different situations appear
	// mostly the same to the caller, in that all of them can be reduced to
	// a set of references regardless of which expression or expressions we
	// derive those from.

	// Our first task then is to select an appropriate implementation based
	// on which address type the reference refers to.
	switch targetAddr := ref.Subject.(type) {
	case addrs.InputVariable:
		return a.metaReferencesInputVariable(moduleAddr, targetAddr, ref.Remaining)
	case addrs.AbsModuleCallOutput:
		return a.metaReferencesOutputValue(moduleAddr, targetAddr, ref.Remaining)
	case addrs.ResourceInstance:
		return a.metaReferencesResourceInstance(moduleAddr, targetAddr, ref.Remaining)
	default:
		// For anything we don't explicitly support we'll just return no
		// references. This includes the reference types that don't really
		// refer to configuration objects at all, like "path.module",
		// and so which cannot possibly generate any references.
		return moduleAddr, nil
	}
}

func (a *Analyzer) metaReferencesInputVariable(calleeAddr addrs.ModuleInstance, addr addrs.InputVariable, remain hcl.Traversal) (addrs.ModuleInstance, []*addrs.Reference) {
	if calleeAddr.IsRoot() {
		// A root module variable definition can never refer to anything,
		// because it conceptually exists outside of any module.
		// We're also returning a technically-incorrect module address in
		// this case, because there isn't a correct one to return, but it
		// doesn't matter because we're returning no references to interpet
		// relative to it anyway.
		return calleeAddr, nil
	}

	callerAddr, callAddr := calleeAddr.Call()

	// We need to find the module call inside the caller module.
	callerCfg := a.ModuleConfig(callerAddr)
	if callerCfg == nil {
		return callerAddr, nil
	}
	call := callerCfg.ModuleCalls[callAddr.Name]
	if call == nil {
		return callerAddr, nil
	}

	// Now we need to look for an attribute matching the variable name inside
	// the module block body.
	body := call.Config
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: addr.Name},
		},
	}
	// We don't check for errors here because we'll make a best effort to
	// analyze whatever partial result HCL is able to extract.
	content, _, _ := body.PartialContent(schema)
	attr := content.Attributes[addr.Name]
	if attr == nil {
		return callerAddr, nil
	}
	refs, _ := lang.ReferencesInExpr(attr.Expr)
	return callerAddr, refs
}

func (a *Analyzer) metaReferencesOutputValue(callerAddr addrs.ModuleInstance, addr addrs.AbsModuleCallOutput, remain hcl.Traversal) (addrs.ModuleInstance, []*addrs.Reference) {
	calleeAddr := callerAddr.Child(addr.Call.Call.Name, addr.Call.Key)

	// We need to find the output value declaration inside the callee module.
	calleeCfg := a.ModuleConfig(calleeAddr)
	if calleeCfg == nil {
		return calleeAddr, nil
	}

	oc := calleeCfg.Outputs[addr.Name]
	if oc == nil {
		return calleeAddr, nil
	}

	// We don't check for errors here because we'll make a best effort to
	// analyze whatever partial result HCL is able to extract.
	refs, _ := lang.ReferencesInExpr(oc.Expr)
	return calleeAddr, refs
}

func (a *Analyzer) metaReferencesResourceInstance(moduleAddr addrs.ModuleInstance, addr addrs.ResourceInstance, remain hcl.Traversal) (addrs.ModuleInstance, []*addrs.Reference) {
	modCfg := a.ModuleConfig(moduleAddr)
	if modCfg == nil {
		return moduleAddr, nil
	}

	rc := modCfg.ResourceByAddr(addr.Resource)
	if rc == nil {
		return moduleAddr, nil
	}

	// In valid cases we should have the schema for this resource type
	// available. In invalid cases we might be dealing with partial information,
	// and so the schema might be nil so we won't be able to return reference
	// information for this particular situation.
	providerSchema := a.providerSchemas[rc.Provider]
	if providerSchema == nil {
		return moduleAddr, nil
	}
	resourceTypeSchema := providerSchema.ResourceTypes[addr.Resource.Type]
	if resourceTypeSchema == nil {
		return moduleAddr, nil
	}

	// When analyzing the resource configuration to look for references, we'll
	// make a best effort to narrow down to only a particular sub-portion of
	// the configuration by following the remaining traversal steps. In the
	// ideal case this will lead us to a specific expression, but as a
	// compromise it might lead us to a nested block where we can then
	// analyze _all_ of the expressions inside.
	body := rc.Config
	schema := resourceTypeSchema
	for _, step := range remain {

	}
}
