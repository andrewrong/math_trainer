package automation

import "testing"

func TestCapabilitiesAdvertiseNewStoryWorkflows(t *testing.T) {
	operations := map[string]map[string]bool{}
	for _, capability := range Capabilities() {
		operations[capability.Name] = map[string]bool{}
		for _, operation := range capability.Operations {
			operations[capability.Name][operation] = true
		}
	}
	for domain, operation := range map[string]string{
		"english": "POST /english/weeks/{weekId}/story/regenerate",
		"chinese": "POST /chinese/context-stories/{setKey}",
	} {
		if !operations[domain][operation] {
			t.Fatalf("%s capability does not advertise %s", domain, operation)
		}
	}
}
