package planner

import "sort"

type Plan struct {
	Install          []string
	Remove           []string
	AlreadyInstalled []string
	DesiredMissing   []string
}

// Build computes a safe package plan. Removal is restricted to packages recorded
// in the previous managed baseline; a missing baseline never triggers removals.
func Build(desired, previouslyManaged, installed []string, hasBaseline bool) Plan {
	return BuildWithRetained(desired, previouslyManaged, installed, nil, hasBaseline)
}

// BuildWithRetained avoids removing previously managed packages that have been
// explicitly classified as foreign inventory rather than repository-managed.
func BuildWithRetained(desired, previouslyManaged, installed, retained []string, hasBaseline bool) Plan {
	desiredSet := set(desired)
	managedSet := set(previouslyManaged)
	installedSet := set(installed)
	retainedSet := set(retained)
	p := Plan{}
	for pkg := range desiredSet {
		if installedSet[pkg] {
			p.AlreadyInstalled = append(p.AlreadyInstalled, pkg)
		} else {
			p.Install = append(p.Install, pkg)
			p.DesiredMissing = append(p.DesiredMissing, pkg)
		}
	}
	if hasBaseline {
		for pkg := range managedSet {
			if !desiredSet[pkg] && !retainedSet[pkg] && installedSet[pkg] {
				p.Remove = append(p.Remove, pkg)
			}
		}
	}
	sort.Strings(p.Install)
	sort.Strings(p.Remove)
	sort.Strings(p.AlreadyInstalled)
	sort.Strings(p.DesiredMissing)
	return p
}

func set(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
