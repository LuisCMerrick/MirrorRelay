package security

import "net/http"

// RepositoryContentSecurityPolicy gives upstream documents an opaque origin.
// Scripts may manipulate the document (including the generated directory UI),
// but cannot access the administration origin, initiate fetches, or start workers.
// Never add allow-same-origin: repository content is not trusted application code.
const RepositoryContentSecurityPolicy = "sandbox allow-downloads allow-scripts; connect-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func SandboxRepositoryResponse(header http.Header) {
	for _, value := range header.Values("Content-Security-Policy") {
		if value == RepositoryContentSecurityPolicy {
			header.Set("X-Content-Type-Options", "nosniff")
			return
		}
	}
	// Multiple CSP policies intersect, retaining any stricter upstream policy.
	header.Add("Content-Security-Policy", RepositoryContentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
}
