// Package browser provides HTML directory index parsing and unified repository browsing.
package browser

import "strings"

// Embedded SVG icons used in repository browser.
var Icons = map[string]string{
	"parent":    `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"></polyline></svg>`,
	"folder":    `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#eab308" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>`,
	"archive":   `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#f97316" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="21 8 21 21 3 21 3 8"></polyline><rect x="1" y="3" width="22" height="5"></rect><line x1="10" y1="12" x2="14" y2="12"></line></svg>`,
	"package":   `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#3b82f6" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16.5 9.4 7.55 4.24a1.78 1.78 0 0 0-2.5 1.55v12.42a1.78 1.78 0 0 0 2.5 1.55L16.5 14.6a1.78 1.78 0 0 0 0-3.2z"></path><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path><polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline><line x1="12" y1="22.08" x2="12" y2="12"></line></svg>`,
	"iso":       `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#8b5cf6" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><circle cx="12" cy="12" r="3"></circle></svg>`,
	"checksum":  `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path><path d="m9 12 2 2 4-4"></path></svg>`,
	"signature": `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#06b6d4" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>`,
	"text":      `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#64748b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line><polyline points="10 9 9 9 8 9"></polyline></svg>`,
	"file":      `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline></svg>`,
}

// GetIconSVG returns the SVG string for a given icon type.
func GetIconSVG(iconType string) string {
	if svg, ok := Icons[iconType]; ok {
		return svg
	}
	return Icons["file"]
}

// DetectIconType classifies a file or directory name into an icon category.
func DetectIconType(name string, isDir bool) string {
	if isDir {
		if name == ".." || name == "../" {
			return "parent"
		}
		return "folder"
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".deb") || strings.HasSuffix(lower, ".rpm") || strings.HasSuffix(lower, ".apk") ||
		strings.HasSuffix(lower, ".whl") || strings.HasSuffix(lower, ".gem") || strings.HasSuffix(lower, ".jar") ||
		strings.HasSuffix(lower, ".opk") || strings.HasSuffix(lower, ".ipk"):
		return "package"
	case strings.HasSuffix(lower, ".iso") || strings.HasSuffix(lower, ".img") || strings.HasSuffix(lower, ".qcow2") || strings.HasSuffix(lower, ".vmdk"):
		return "iso"
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tbz2") || strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") ||
		strings.HasSuffix(lower, ".tar.zst") || strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".7z") ||
		strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".xz") || strings.HasSuffix(lower, ".bz2"):
		return "archive"
	case strings.HasSuffix(lower, "sums") || strings.HasSuffix(lower, ".sha256") || strings.HasSuffix(lower, ".sha512") ||
		strings.HasSuffix(lower, ".md5") || strings.HasSuffix(lower, ".sha1") || lower == "release" || lower == "inrelease":
		return "checksum"
	case strings.HasSuffix(lower, ".asc") || strings.HasSuffix(lower, ".sig") || strings.HasSuffix(lower, ".gpg") || lower == "release.gpg":
		return "signature"
	case strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".log") ||
		strings.HasSuffix(lower, ".cfg") || strings.HasSuffix(lower, ".conf") || strings.HasSuffix(lower, ".yaml") ||
		strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".xml") ||
		strings.HasSuffix(lower, ".repo") || strings.HasSuffix(lower, ".list"):
		return "text"
	default:
		return "file"
	}
}
