package works

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func ValidID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) && strings.IndexFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

func textValue(value string, limit int, required, multiline bool) (string, error) {
	value = strings.TrimSpace(value)
	if (required && value == "") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t'))
	}) >= 0 {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid work text value")
	}
	return value, nil
}

// NormalizeCreate 只检查通用资料形状，已注册类型及角色由宿主服务校验。
func NormalizeCreate(request CreateRequest) (CreateRequest, error) {
	if !contenttype.ValidID(request.ContentType) {
		return request, domain.Fail("INVALID_ARGUMENT", "Invalid work content type")
	}
	var err error
	if request.Title, err = textValue(request.Title, MaxTitleLength, true, false); err != nil {
		return request, err
	}
	if request.Description, err = textValue(request.Description, MaxDescriptionLength, false, true); err != nil {
		return request, err
	}
	if len(request.ExternalIDs) > MaxExternalIDs || len(request.Credits) > MaxCredits || len(request.Tags) > MaxTags {
		return request, domain.Fail("INVALID_ARGUMENT", "Work metadata limit exceeded")
	}
	request.ExternalIDs = append([]ExternalID{}, request.ExternalIDs...)
	request.Credits = append([]Credit{}, request.Credits...)
	request.Tags = append([]string{}, request.Tags...)
	externalIDs, credits, tags := map[ExternalID]bool{}, map[Credit]bool{}, map[string]bool{}
	for i, item := range request.ExternalIDs {
		if !contenttype.ValidID(item.Namespace) {
			return request, domain.Fail("INVALID_ARGUMENT", "Invalid external identifier namespace")
		}
		if item.Value, err = textValue(item.Value, 256, true, false); err != nil {
			return request, err
		}
		if externalIDs[item] {
			return request, domain.Fail("INVALID_ARGUMENT", "Duplicate work external identifier")
		}
		externalIDs[item], request.ExternalIDs[i] = true, item
	}
	for i, item := range request.Credits {
		if !contenttype.ValidID(item.Role) {
			return request, domain.Fail("INVALID_ARGUMENT", "Invalid work credit role")
		}
		if item.Name, err = textValue(item.Name, 256, true, false); err != nil {
			return request, err
		}
		if credits[item] {
			return request, domain.Fail("INVALID_ARGUMENT", "Duplicate work credit")
		}
		credits[item], request.Credits[i] = true, item
	}
	for i, item := range request.Tags {
		if item, err = textValue(item, 64, true, false); err != nil {
			return request, err
		}
		if tags[item] {
			return request, domain.Fail("INVALID_ARGUMENT", "Duplicate work tag")
		}
		tags[item], request.Tags[i] = true, item
	}
	return request, nil
}

func NormalizeUpdate(request UpdateRequest) (UpdateRequest, error) {
	if request.ExpectedRevision < 1 || request.ExpectedRevision == math.MaxInt64 {
		return request, domain.Fail("INVALID_ARGUMENT", "Invalid expected work revision")
	}
	normalized, err := NormalizeCreate(CreateRequest{ContentType: "work", Title: request.Title, Description: request.Description, ExternalIDs: request.ExternalIDs, Credits: request.Credits, Tags: request.Tags})
	if err != nil {
		return request, err
	}
	return UpdateRequest{ExpectedRevision: request.ExpectedRevision, ShowInTrackList: request.ShowInTrackList, Title: normalized.Title, Description: normalized.Description, ExternalIDs: normalized.ExternalIDs, Credits: normalized.Credits, Tags: normalized.Tags}, nil
}

func NormalizeQuery(query Query) (Query, error) {
	if query.PackageKind != "" && query.PackageKind != "directory" && query.PackageKind != "cd_dump" {
		return query, domain.Fail("INVALID_ARGUMENT", "Invalid work package kind filter")
	}
	if (query.ContentType != "" && !contenttype.ValidID(query.ContentType)) || (query.After != "" && !ValidID(query.After)) || query.Limit < 1 || query.Limit > MaxPageSize+1 {
		return query, domain.Fail("INVALID_ARGUMENT", "Invalid work query")
	}
	var err error
	if query.Q, err = textValue(query.Q, 256, false, false); err != nil {
		return query, err
	}
	if query.Tag, err = textValue(query.Tag, 64, false, false); err != nil {
		return query, err
	}
	query.Credit, err = textValue(query.Credit, 256, false, false)
	return query, err
}
