package cli

import (
	"sort"

	"github.com/T-Onix/zacman/internal/pacman"
	"github.com/T-Onix/zacman/internal/planner"
)

// filterInstalledProviderTargets removes targets which Pacman already considers
// satisfied by installed packages' Provides metadata. We intentionally leave the
// raw installed package set unchanged in planner.BuildWithRetained so removal
// transactions only target actual installed package names, never virtual aliases.
func filterInstalledProviderTargets(client pacman.Client, plan planner.Plan) (planner.Plan, error) {
	if len(plan.Install) == 0 {
		return plan, nil
	}
	satisfied, err := client.SatisfiedByInstalledProviders(plan.Install)
	if err != nil {
		return planner.Plan{}, err
	}
	if len(satisfied) == 0 {
		return plan, nil
	}
	satisfiedSet := makeSet(satisfied)
	remaining := make([]string, 0, len(plan.Install))
	for _, target := range plan.Install {
		if !satisfiedSet[target] {
			remaining = append(remaining, target)
		}
	}
	plan.Install = remaining
	plan.DesiredMissing = append([]string(nil), remaining...)
	plan.AlreadyInstalled = append(plan.AlreadyInstalled, satisfied...)
	sort.Strings(plan.AlreadyInstalled)
	return plan, nil
}
