package server

// workspaceDefaultApplicationPreparationBlocks reports whether an accepted
// default-application command still holds the Workspace's resources. The
// deployment execution engine is retired, so no operation progress write
// remains here; this predicate only answers the retained deletion fence.
func workspaceDefaultApplicationPreparationBlocks(row map[string]any) bool {
	if stringValue(row["action"]) != workspaceDefaultApplicationAction {
		return false
	}
	request, err := decodeWorkspaceDefaultApplication(row)
	return err != nil || request.Phase == "preparing_credentials"
}
