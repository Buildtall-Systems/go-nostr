package nip29

import (
	"slices"

	"github.com/nbd-wtf/go-nostr"
)

func (group Group) GetRoleByName(name string) *Role {
	idx := slices.IndexFunc(group.Roles, func(role *Role) bool { return role.Name == name })
	if idx == -1 {
		return &Role{Name: name}
	} else {
		return group.Roles[idx]
	}
}

// findTag returns the first tag named name, whatever its length; Tags.Find
// skips tags without a value, such as ["private"] or an empty
// ["supported_kinds"].
func findTag(tags nostr.Tags, name string) nostr.Tag {
	for _, tag := range tags {
		if len(tag) >= 1 && tag[0] == name {
			return tag
		}
	}
	return nil
}
