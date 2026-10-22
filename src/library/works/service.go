package works

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
)

// Service 校验插件声明允许的写入语义，读取已有资料不依赖类型是否仍注册。
type Service struct {
	Repository
	types *contenttypes.Registry
}

func New(repository Repository, registry *contenttypes.Registry) *Service {
	return &Service{Repository: repository, types: registry}
}

func (s *Service) ContentTypes() []contenttype.Descriptor { return s.types.List() }

func (s *Service) CreateWork(ctx context.Context, owner string, input CreateRequest, key string) (Work, error) {
	normalized, err := NormalizeCreate(input)
	if err != nil {
		return Work{}, err
	}
	if err = s.validateType(normalized.ContentType, normalized.Credits, normalized.ExternalIDs); err != nil {
		return Work{}, err
	}
	return s.Repository.CreateWork(ctx, owner, normalized, key)
}

func (s *Service) UpdateWork(ctx context.Context, owner, id string, input UpdateRequest, key string) (Work, error) {
	normalized, err := NormalizeUpdate(input)
	if err != nil {
		return Work{}, err
	}
	current, err := s.Repository.Work(ctx, owner, id)
	if err != nil {
		return Work{}, err
	}
	if err = s.validateType(current.ContentType, normalized.Credits, normalized.ExternalIDs); err != nil {
		return Work{}, err
	}
	return s.Repository.UpdateWork(ctx, owner, id, normalized, key)
}

func (s *Service) validateType(id string, credits []Credit, externalIDs []ExternalID) error {
	descriptor, ok := s.types.Get(id)
	if !ok {
		return domain.Fail("UNSUPPORTED", "Content type is not registered for editing")
	}
	contains := func(values []contenttype.NamedValue, key string) bool {
		for _, value := range values {
			if value.ID == key {
				return true
			}
		}
		return false
	}
	for _, credit := range credits {
		if !contains(descriptor.CreditRoles, credit.Role) {
			return domain.Fail("INVALID_ARGUMENT", "Credit role is not declared by the content type")
		}
	}
	for _, externalID := range externalIDs {
		if !contains(descriptor.ExternalIDNamespaces, externalID.Namespace) {
			return domain.Fail("INVALID_ARGUMENT", "External ID namespace is not declared by the content type")
		}
	}
	return nil
}
