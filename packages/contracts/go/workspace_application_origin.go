package contracts

import (
	"crypto/sha1"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
)

const workspaceApplicationOriginDigestLength = 12

// WorkspaceApplicationOriginLabel derives the application binding label from
// original identities. It is byte-compatible with already published origins.
func WorkspaceApplicationOriginLabel(workspaceID, applicationID string) (string, bool) {
	if workspaceID != strings.TrimSpace(workspaceID) || applicationID != strings.TrimSpace(applicationID) {
		return "", false
	}
	if _, _, ok := workspaceOriginDNSIdentity(workspaceID); !ok || !workspaceOriginDNSLabel(applicationID) {
		return "", false
	}
	hash := sha1.New()
	for _, part := range []string{"workspace-application-origin", workspaceID, applicationID} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:workspaceApplicationOriginDigestLength], true
}

// WorkspaceApplicationOriginHost encodes one binding into a single DNS label.
// Existing DNS-safe identities retain <workspace>-<digest>. Native
// workspace_<32hex> identities use w-<32hex>-n<digest>; the n marker separates
// this namespace from every retained hostname without changing the original ID.
func WorkspaceApplicationOriginHost(workspaceID, applicationID, domain string) (string, bool) {
	if workspaceID != strings.TrimSpace(workspaceID) {
		return "", false
	}
	identity, native, ok := workspaceOriginDNSIdentity(workspaceID)
	if !ok {
		return "", false
	}
	label, ok := WorkspaceApplicationOriginLabel(workspaceID, applicationID)
	if !ok {
		return "", false
	}
	if native {
		label = "n" + label
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !workspaceOriginDNSHost(domain) {
		return "", false
	}
	host := identity + "-" + label + "." + domain
	if !workspaceOriginDNSHost(host) {
		return "", false
	}
	return host, true
}

// ParseWorkspaceApplicationOriginHost reverses only the two canonical origin
// formats. The caller must still verify the label against its confirmed binding.
func ParseWorkspaceApplicationOriginHost(host, domain string) (workspaceID, applicationLabel string, ok bool) {
	if host != strings.TrimSpace(host) {
		return "", "", false
	}
	host = strings.ToLower(host)
	if name, port, err := net.SplitHostPort(host); err == nil {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", "", false
		}
		host = name
	} else if strings.Contains(host, ":") {
		return "", "", false
	}
	host = strings.TrimSuffix(host, ".")
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !workspaceOriginDNSHost(domain) || !workspaceOriginDNSHost(host) || !strings.HasSuffix(host, "."+domain) {
		return "", "", false
	}
	binding := strings.TrimSuffix(host, "."+domain)
	if strings.Contains(binding, ".") {
		return "", "", false
	}
	index := strings.LastIndex(binding, "-")
	if index <= 0 {
		return "", "", false
	}
	identity, label := binding[:index], binding[index+1:]
	switch {
	case len(label) == workspaceApplicationOriginDigestLength && workspaceOriginHex(label):
		if !workspaceOriginDNSLabel(identity) {
			return "", "", false
		}
		return identity, label, true
	case len(label) == workspaceApplicationOriginDigestLength+1 && label[0] == 'n' && workspaceOriginHex(label[1:]):
		if len(identity) != 34 || !strings.HasPrefix(identity, "w-") || !workspaceOriginHex(identity[2:]) {
			return "", "", false
		}
		return "workspace_" + identity[2:], label[1:], true
	default:
		return "", "", false
	}
}

func workspaceOriginDNSIdentity(id string) (label string, native, ok bool) {
	if len(id) == 42 && strings.HasPrefix(id, "workspace_") && workspaceOriginHex(id[10:]) {
		return "w-" + id[10:], true, true
	}
	if workspaceOriginDNSLabel(id) {
		return id, false, true
	}
	return "", false, false
}

func workspaceOriginHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func workspaceOriginDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if char != '-' || index == 0 || index == len(value)-1 {
			return false
		}
	}
	return true
}

func workspaceOriginDNSHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !workspaceOriginDNSLabel(label) {
			return false
		}
	}
	return true
}
