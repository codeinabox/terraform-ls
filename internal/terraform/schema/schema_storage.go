package schema

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/hashicorp/go-version"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-ls/internal/terraform/exec"
	"golang.org/x/sync/semaphore"
)

type Reader interface {
	ProviderConfigSchema(name string) (*tfjson.Schema, error)
	Providers() ([]ProviderIdentity, error)
	ResourceSchema(rType string) (*tfjson.Schema, error)
	Resources() ([]Resource, error)
	DataSourceSchema(dsType string) (*tfjson.Schema, error)
	DataSources() ([]DataSource, error)
}

type Writer interface {
	ObtainSchemasForModule(context.Context, *exec.Executor, string) error
}

type Resource struct {
	Name            string
	Provider        ProviderIdentity
	Description     string
	DescriptionKind tfjson.SchemaDescriptionKind
}

type DataSource struct {
	Name            string
	Provider        ProviderIdentity
	Description     string
	DescriptionKind tfjson.SchemaDescriptionKind
}

type StorageFactory func(v string) (*Storage, error)

type Storage struct {
	ps        *tfjson.ProviderSchemas
	converter ProviderIdentityConverter
	logger    *log.Logger

	// sem ensures atomic reading and obtaining of schemas
	// as the process of obtaining it may not be thread-safe
	sem *semaphore.Weighted
}

var defaultLogger = log.New(ioutil.Discard, "", 0)

func newStorage() *Storage {
	return &Storage{
		logger: defaultLogger,
		sem:    semaphore.NewWeighted(1),
	}
}

func NewStorageForVersion(vs string) (*Storage, error) {
	ver, err := version.NewVersion(vs)
	if err != nil {
		return nil, err
	}

	v0_13 := version.Must(version.NewVersion("0.13.0"))
	if ver.GreaterThanOrEqual(v0_13) {
		s := newStorage()
		s.converter = v013ProviderIdentityConverter{}
		return s, nil
	}

	v0_12 := version.Must(version.NewVersion("0.12.0"))
	if ver.GreaterThanOrEqual(v0_12) {
		s := newStorage()
		s.converter = v012ProviderIdentityConverter{}
		return s, nil
	}

	return nil, fmt.Errorf("no schema storage available for terraform %s", vs)
}

func (s *Storage) SetLogger(logger *log.Logger) {
	s.logger = logger
}

// ObtainSchemasForModule will obtain schema via tf
// and store it for later consumption via Reader methods
func (s *Storage) ObtainSchemasForModule(ctx context.Context, tf *exec.Executor, dir string) error {
	return s.obtainSchemasForModule(ctx, tf, dir)
}

func (s *Storage) obtainSchemasForModule(ctx context.Context, tf *exec.Executor, dir string) error {
	s.logger.Printf("Acquiring semaphore before retrieving schema for %q ...", dir)
	err := s.sem.Acquire(context.Background(), 1)
	if err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer s.sem.Release(1)

	tf.SetWorkdir(dir)

	s.logger.Printf("Retrieving schemas for %q ...", dir)
	start := time.Now()
	ps, err := tf.ProviderSchemas(ctx)
	if err != nil {
		return fmt.Errorf("Unable to retrieve schemas for %q: %w", dir, err)
	}
	s.ps = ps
	s.logger.Printf("Schemas retrieved for %q in %s", dir, time.Since(start))
	return nil
}

func (s *Storage) schema() (*tfjson.ProviderSchemas, error) {
	s.logger.Println("Acquiring semaphore before reading schema")
	acquired := s.sem.TryAcquire(1)
	if !acquired {
		return nil, fmt.Errorf("schema temporarily unavailable")
	}
	defer s.sem.Release(1)

	if s.ps == nil {
		return nil, &NoSchemaAvailableErr{}
	}
	return s.ps, nil
}

func (s *Storage) ProviderConfigSchema(rawIdentity string) (*tfjson.Schema, error) {
	identity := s.converter.RawToQualifiedName(rawIdentity)
	name := ProviderIdentity{identity: identity, converter: s.converter}

	s.logger.Printf("Reading %q provider schema", name)

	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	schema, ok := ps.Schemas[name.QualifiedName()]
	if !ok {
		return nil, &SchemaUnavailableErr{"provider", name.QualifiedName()}
	}

	if schema.ConfigSchema == nil {
		return nil, &SchemaUnavailableErr{"provider", name.QualifiedName()}
	}

	return schema.ConfigSchema, nil
}

func (s *Storage) Providers() ([]ProviderIdentity, error) {
	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	providers := make([]ProviderIdentity, 0)
	for rawName := range ps.Schemas {
		identity := s.converter.QualifiedNameToRaw(rawName)
		name := ProviderIdentity{identity: identity, converter: s.converter}
		providers = append(providers, name)
	}

	return providers, nil
}

func (s *Storage) ResourceSchema(rType string) (*tfjson.Schema, error) {
	s.logger.Printf("Reading %q resource schema", rType)

	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	// TODO: Reflect provider alias associations here
	// (need to be parsed and made accessible first)
	for _, schema := range ps.Schemas {
		rSchema, ok := schema.ResourceSchemas[rType]
		if ok {
			return rSchema, nil
		}
	}

	return nil, &SchemaUnavailableErr{"resource", rType}
}

func (s *Storage) Resources() ([]Resource, error) {
	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	resources := make([]Resource, 0)
	for provider, schema := range ps.Schemas {
		for name, r := range schema.ResourceSchemas {
			resources = append(resources, Resource{
				Provider: ProviderIdentity{
					identity:  provider,
					converter: s.converter,
				},
				Name:        name,
				Description: r.Block.Description,
			})
		}
	}

	return resources, nil
}

func (s *Storage) DataSourceSchema(dsType string) (*tfjson.Schema, error) {
	s.logger.Printf("Reading %q datasource schema", dsType)

	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	// TODO: Reflect provider alias associations here
	// (need to be parsed and made accessible first)
	for _, schema := range ps.Schemas {
		rSchema, ok := schema.DataSourceSchemas[dsType]
		if ok {
			return rSchema, nil
		}
	}

	return nil, &SchemaUnavailableErr{"data", dsType}
}

func (s *Storage) DataSources() ([]DataSource, error) {
	ps, err := s.schema()
	if err != nil {
		return nil, err
	}

	dataSources := make([]DataSource, 0)
	for provider, schema := range ps.Schemas {
		for name, d := range schema.DataSourceSchemas {
			dataSources = append(dataSources, DataSource{
				Provider: ProviderIdentity{
					identity:  provider,
					converter: s.converter,
				},
				Name:        name,
				Description: d.Block.Description,
			})
		}
	}

	return dataSources, nil
}
