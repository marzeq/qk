package sema

import (
	"sort"
	"strings"
)

// MethodTarget identifies an implementation of a nominal method requirement.
// Specialization uses these targets directly from the current analysis.
type MethodTarget struct {
	Module      string
	Name        string
	OwnerModule string
	OwnerName   string
	LookupName  string
}

func (a *Analyser) MethodTargets() []MethodTarget {
	var targets []MethodTarget
	for ownerKey, methods := range a.methods {
		for lookupName, method := range methods {
			ownerModule, ownerName := "", ""
			if method.MethodOwnerType != nil {
				module, name, _, nominal := methodOwnerIdentity(method.MethodOwnerType)
				if nominal {
					ownerModule, ownerName = module, name
				}
			}
			if ownerModule == "" {
				if split := strings.LastIndex(ownerKey, ":"); split >= 0 {
					ownerModule, ownerName = ownerKey[:split], ownerKey[split+1:]
				}
			}
			targets = append(targets, MethodTarget{
				Module: method.DefinitionModule, Name: method.Name,
				OwnerModule: ownerModule, OwnerName: ownerName, LookupName: lookupName,
			})
		}
	}
	for lookupName, candidates := range a.structuralMethods {
		for _, method := range candidates {
			if method.MethodOwnerType == nil {
				continue
			}
			ownerModule, ownerName, _, nominal := methodOwnerIdentity(method.MethodOwnerType)
			if !nominal {
				continue
			}
			targets = append(targets, MethodTarget{
				Module: method.DefinitionModule, Name: method.Name,
				OwnerModule: ownerModule, OwnerName: ownerName, LookupName: lookupName,
			})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		left, right := targets[i], targets[j]
		if left.OwnerModule != right.OwnerModule {
			return left.OwnerModule < right.OwnerModule
		}
		if left.OwnerName != right.OwnerName {
			return left.OwnerName < right.OwnerName
		}
		if left.LookupName != right.LookupName {
			return left.LookupName < right.LookupName
		}
		if left.Module != right.Module {
			return left.Module < right.Module
		}
		return left.Name < right.Name
	})
	return targets
}
