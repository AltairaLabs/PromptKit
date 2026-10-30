package config

// GetAPIVersion returns the manifest apiVersion; part of the K8s manifest
// interface implementation for ProviderConfig.
func (c *ProviderConfig) GetAPIVersion() string {
	return c.APIVersion
}

// GetKind returns the manifest kind.
func (c *ProviderConfig) GetKind() string {
	return c.Kind
}

// GetName returns the manifest metadata.name.
func (c *ProviderConfig) GetName() string {
	return c.Metadata.Name
}

// SetID sets spec.id.
func (c *ProviderConfig) SetID(id string) {
	c.Spec.ID = id
}

// GetID returns the explicit spec.id, empty when the manifest omits it.
func (c *ProviderConfig) GetID() string {
	return c.Spec.ID
}

// GetAPIVersion returns the manifest apiVersion; part of the K8s manifest
// interface implementation for ProviderConfigK8s.
func (c *ProviderConfigK8s) GetAPIVersion() string {
	return c.APIVersion
}

// GetKind returns the manifest kind.
func (c *ProviderConfigK8s) GetKind() string {
	return c.Kind
}

// GetName returns the manifest metadata.name.
func (c *ProviderConfigK8s) GetName() string {
	return c.Metadata.Name
}

// SetID sets spec.id.
func (c *ProviderConfigK8s) SetID(id string) {
	c.Spec.ID = id
}

// GetID returns the explicit spec.id, empty when the manifest omits it.
func (c *ProviderConfigK8s) GetID() string {
	return c.Spec.ID
}
