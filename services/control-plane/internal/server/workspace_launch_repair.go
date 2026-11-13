package server

import (
	"strings"
)

// repairWorkspaceLaunchRuntime replaces only the failed Runtime of an already
// paid Launch. The original Key, debit, Compute, Storage, Attachment and Secret
// identities are read from the persisted Launch and cannot be supplied by the
// operator.

func workspaceImageReferenceWithDigest(value string) bool {
	repository, digest, ok := strings.Cut(strings.TrimSpace(value), "@")
	return ok && repository != "" && !strings.Contains(repository, "@") && workspaceImageDigestPattern.MatchString(digest)
}
