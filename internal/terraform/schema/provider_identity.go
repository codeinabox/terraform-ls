package schema

import "strings"

const defaultBaseIdentity = "registry.terraform.io/hashicorp/"

type ProviderIdentity struct {
	identity string
	converter ProviderIdentityConverter
}

func (pi ProviderIdentity) String() string {
	return pi.QualifiedName()
}

func (pi ProviderIdentity) RawName() string {
	return pi.converter.QualifiedNameToRaw(pi.identity)
}

func (pi ProviderIdentity) QualifiedName() string {
	return pi.identity
}

type ProviderIdentityConverter interface {
	QualifiedNameToRaw(string) string
	RawToQualifiedName(string) string
}

type v012ProviderIdentityConverter struct{}

func (pic v012ProviderIdentityConverter) QualifiedNameToRaw(name string) string {
	return name
}

func (pic v012ProviderIdentityConverter) RawToQualifiedName(name string) string {
	return name
}

type v013ProviderIdentityConverter struct{}

func (pic v013ProviderIdentityConverter) QualifiedNameToRaw(name string) string {
	return strings.TrimPrefix(name, defaultBaseIdentity)
}

func (pic v013ProviderIdentityConverter) RawToQualifiedName(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return defaultBaseIdentity+name
}
