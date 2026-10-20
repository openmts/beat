package agentrolloutremote

import "time"

const (
	ManifestAssetName  = "beat-agent-manifest.json"
	SignatureAssetName = "beat-agent-manifest.sig"
	SigstoreAssetName  = "beat-agent-manifest.sigstore.json"
)

type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Release struct {
	TagName     string    `json:"tag_name"`
	Version     string    `json:"version"`
	Channel     string    `json:"channel"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Ready       bool      `json:"ready"`
	Reason      string    `json:"reason"`
	Assets      []Asset   `json:"assets"`
}

type Page struct {
	Repository string    `json:"repository"`
	Items      []Release `json:"items"`
	NextPage   *int      `json:"next_page,omitempty"`
}
