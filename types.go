package main

// ---------------- Clash API payload types ----------------

type apiVersion struct {
	Meta    bool   `json:"meta"`
	Premium bool   `json:"premium"`
	Version string `json:"version"`
}

type apiConnections struct {
	UploadTotal   int64        `json:"uploadTotal"`
	DownloadTotal int64        `json:"downloadTotal"`
	Connections   []connObject `json:"connections"`
	Memory        int64        `json:"memory"`
}

type connMetadata struct {
	Network string `json:"network"`
	Type    string `json:"type"`
}

type connObject struct {
	Chains   []string     `json:"chains"`
	Rule     string       `json:"rule"`
	Upload   int64        `json:"upload"`
	Download int64        `json:"download"`
	Metadata connMetadata `json:"metadata"`
}

type apiProxies struct {
	Proxies map[string]proxyObject `json:"proxies"`
}

type proxyObject struct {
	Type    string        `json:"type"`
	Name    string        `json:"name"`
	Now     string        `json:"now"`
	History []historyItem `json:"history"`
}

type historyItem struct {
	Time  string `json:"time"`
	Delay int64  `json:"delay"`
}
