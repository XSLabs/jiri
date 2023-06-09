package gitutil

import (
	"fmt"
	"regexp"
)

func SubmodulePathFromStatus(submoduleStatus []string) ([]string, error) {
	var submoduleConfigRegex = regexp.MustCompile(`([-+U]?)([a-fA-F0-9]{40})\s([^\s]*)\s?`)
	submodulePaths := []string{}
	for _, subm := range submoduleStatus {
		submConfig := submoduleConfigRegex.FindStringSubmatch(subm)
		if len(submConfig) != 4 {
			return nil, fmt.Errorf("expected substring to have length of 4, but got %d", len(submConfig))
		}
		// Check if submodules are initialized. If not intialized, add to the list.
		if submConfig[1] == "-" {
			submodulePaths = append(submodulePaths, submConfig[3])
		}
	}
	return submodulePaths, nil
}
