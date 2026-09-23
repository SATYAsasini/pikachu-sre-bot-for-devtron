package tools

// All builds the registry of every tool compiled into the binary. There are,
// by construction, no write tools to register.
func All() *Registry {
	r := NewRegistry()
	r.Register(K8sTools()...)
	r.Register(MetricsTools()...)
	r.Register(AlertTools()...)
	r.Register(DiscoveryTools()...)
	r.Register(KnowledgeTools()...)
	r.Register(DevtronTools()...)
	return r
}
