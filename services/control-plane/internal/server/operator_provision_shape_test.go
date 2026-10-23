package server

import "testing"

// TestOperatorProvisionShapeAcceptsAnAbsentName pins the retained owner's name
// contract: absence derives the Tenant name from the email, while an explicit
// null, empty, padded or over-long name is refused. The BFF must therefore
// forward an absent browser name as an absent field.
func TestOperatorProvisionShapeAcceptsAnAbsentName(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"email":     "customer@example.com",
			"password":  "correct-horse-battery-staple",
			"admission": "full_cloud_customer",
		}
	}
	if !operatorProvisionShapeValid(base()) {
		t.Fatal("absent name must be accepted")
	}
	for name, mutate := range map[string]func(map[string]any){
		"null":    func(input map[string]any) { input["name"] = nil },
		"empty":   func(input map[string]any) { input["name"] = "" },
		"padded":  func(input map[string]any) { input["name"] = " Customer " },
		"unknown": func(input map[string]any) { input["name"] = 7 },
	} {
		input := base()
		mutate(input)
		if operatorProvisionShapeValid(input) {
			t.Fatalf("%s name must be refused", name)
		}
	}
}
