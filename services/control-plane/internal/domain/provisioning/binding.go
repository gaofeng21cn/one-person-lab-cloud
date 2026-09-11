package provisioning

// Application binding states name the current application binding carried by
// the Workspace projection. Loop-two application deployments replace the
// fixed OPL App value with a revision identity.
const (
	// ApplicationBindingEmpty marks a resource-ready Workspace with no
	// application installed.
	ApplicationBindingEmpty = "empty"
	// ApplicationBindingOPLApp marks the retained fixed OPL App binding that
	// every full Launch establishes.
	ApplicationBindingOPLApp = "opl_app"
)
