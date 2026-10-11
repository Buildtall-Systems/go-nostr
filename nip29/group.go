package nip29

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/nbd-wtf/go-nostr"
)

type GroupAddress struct {
	Relay string
	ID    string
}

func (gid GroupAddress) String() string {
	p, _ := url.Parse(gid.Relay)
	return fmt.Sprintf("%s'%s", p.Host, gid.ID)
}

func (gid GroupAddress) IsValid() bool {
	return gid.Relay != "" && gid.ID != ""
}

func (gid GroupAddress) Equals(gid2 GroupAddress) bool {
	return gid.Relay == gid2.Relay && gid.ID == gid2.ID
}

func ParseGroupAddress(raw string) (GroupAddress, error) {
	spl := strings.Split(raw, "'")
	if len(spl) != 2 {
		return GroupAddress{}, fmt.Errorf("invalid group id")
	}
	return GroupAddress{ID: spl[1], Relay: nostr.NormalizeURL(spl[0])}, nil
}

type Group struct {
	Address GroupAddress

	Name       string
	Picture    string
	Banner     string
	About      string
	Members    map[string][]*Role
	Private    bool
	Closed     bool
	Restricted bool
	Hidden     bool

	// SupportedKinds is nil when the group supports every kind, and empty
	// when it supports none.
	SupportedKinds []int

	Roles []*Role

	// Pins holds the "e" and "a" tags of the pinned events, in display order.
	Pins nostr.Tags

	LastMetadataUpdate nostr.Timestamp
	LastAdminsUpdate   nostr.Timestamp
	LastMembersUpdate  nostr.Timestamp
	LastRolesUpdate    nostr.Timestamp
	LastPinsUpdate     nostr.Timestamp
}

func (group Group) String() string {
	maybePrivate := ""
	maybeClosed := ""

	if group.Private {
		maybePrivate = " private"
	}
	if group.Closed {
		maybeClosed = " closed"
	}

	members := make([]string, len(group.Members))
	i := 0
	for pubkey, roles := range group.Members {
		members[i] = pubkey
		if len(roles) > 0 {
			members[i] += ":"
		}
		for _, role := range roles {
			members[i] += role.Name
			if slices.Contains(group.Roles, role) {
				members[i] += "*"
			}
			members[i] += "/"
		}
		members[i] = strings.TrimRight(members[i], "/")
		i++
	}

	return fmt.Sprintf(`<Group %s name="%s"%s%s picture="%s" about="%s" members=[%v]>`,
		group.Address,
		group.Name,
		maybePrivate,
		maybeClosed,
		group.Picture,
		group.About,
		strings.Join(members, " "),
	)
}

// NewGroup takes a group address in the form "<id>'<relay-hostname>"
func NewGroup(gadstr string) (Group, error) {
	gad, err := ParseGroupAddress(gadstr)
	if err != nil {
		return Group{}, fmt.Errorf("invalid group id '%s': %w", gadstr, err)
	}

	return Group{
		Address: gad,
		Name:    gad.ID,
		Members: make(map[string][]*Role),
	}, nil
}

func NewGroupFromMetadataEvent(relayURL string, evt *nostr.Event) (Group, error) {
	g := Group{
		Address: GroupAddress{
			Relay: relayURL,
			ID:    evt.Tags.GetD(),
		},
		Name:    evt.Tags.GetD(),
		Members: make(map[string][]*Role),
	}

	err := g.MergeInMetadataEvent(evt)
	return g, err
}

func (group Group) ToMetadataEvent() *nostr.Event {
	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: group.LastMetadataUpdate,
		Tags: nostr.Tags{
			nostr.Tag{"d", group.Address.ID},
		},
	}
	if group.Name != "" {
		evt.Tags = append(evt.Tags, nostr.Tag{"name", group.Name})
	}
	if group.About != "" {
		evt.Tags = append(evt.Tags, nostr.Tag{"about", group.About})
	}
	if group.Picture != "" {
		evt.Tags = append(evt.Tags, nostr.Tag{"picture", group.Picture})
	}
	if group.Banner != "" {
		evt.Tags = append(evt.Tags, nostr.Tag{"banner", group.Banner})
	}

	// status: each flag is present when set and omitted otherwise
	if group.Private {
		evt.Tags = append(evt.Tags, nostr.Tag{"private"})
	}
	if group.Closed {
		evt.Tags = append(evt.Tags, nostr.Tag{"closed"})
	}
	if group.Restricted {
		evt.Tags = append(evt.Tags, nostr.Tag{"restricted"})
	}
	if group.Hidden {
		evt.Tags = append(evt.Tags, nostr.Tag{"hidden"})
	}

	if group.SupportedKinds != nil {
		tag := make(nostr.Tag, 1, 1+len(group.SupportedKinds))
		tag[0] = "supported_kinds"
		for _, kind := range group.SupportedKinds {
			tag = append(tag, strconv.Itoa(kind))
		}
		evt.Tags = append(evt.Tags, tag)
	}

	return evt
}

func (group Group) ToAdminsEvent() *nostr.Event {
	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupAdmins,
		CreatedAt: group.LastAdminsUpdate,
		Tags:      make(nostr.Tags, 1, 1+len(group.Members)/3),
	}
	evt.Tags[0] = nostr.Tag{"d", group.Address.ID}

	for _, member := range slices.Sorted(maps.Keys(group.Members)) {
		roles := group.Members[member]
		if len(roles) == 0 {
			// is not an admin
			continue
		}

		// is an admin
		tag := make([]string, 2, 2+len(roles))
		tag[0] = "p"
		tag[1] = member
		for _, role := range roles {
			tag = append(tag, role.Name)
		}
		evt.Tags = append(evt.Tags, tag)
	}

	return evt
}

func (group Group) ToMembersEvent() *nostr.Event {
	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupMembers,
		CreatedAt: group.LastMembersUpdate,
		Tags:      make(nostr.Tags, 1, 1+len(group.Members)),
	}
	evt.Tags[0] = nostr.Tag{"d", group.Address.ID}

	for _, member := range slices.Sorted(maps.Keys(group.Members)) {
		// include both admins and normal members
		evt.Tags = append(evt.Tags, nostr.Tag{"p", member})
	}

	return evt
}

func (group Group) ToRolesEvent() *nostr.Event {
	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupRoles,
		CreatedAt: group.LastRolesUpdate,
		Tags:      make(nostr.Tags, 1, 1+len(group.Roles)),
	}
	evt.Tags[0] = nostr.Tag{"d", group.Address.ID}

	for _, role := range group.Roles {
		evt.Tags = append(evt.Tags, nostr.Tag{"role", role.Name, role.Description})
	}

	return evt
}

func (group Group) ToPinsEvent() *nostr.Event {
	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupPinnedEvents,
		CreatedAt: group.LastPinsUpdate,
		Tags:      make(nostr.Tags, 1, 1+len(group.Pins)),
	}
	evt.Tags[0] = nostr.Tag{"d", group.Address.ID}

	for _, pin := range group.Pins {
		evt.Tags = append(evt.Tags, slices.Clone(pin))
	}

	return evt
}

func (group *Group) MergeInMetadataEvent(evt *nostr.Event) error {
	if evt.Kind != nostr.KindSimpleGroupMetadata {
		return fmt.Errorf("expected kind %d, got %d", nostr.KindSimpleGroupMetadata, evt.Kind)
	}
	if evt.CreatedAt < group.LastMetadataUpdate {
		return fmt.Errorf("event is older than our last update (%d vs %d)", evt.CreatedAt, group.LastMetadataUpdate)
	}

	// parse before any change, so a malformed event leaves the group as it was
	var supportedKinds []int
	if tag := findTag(evt.Tags, "supported_kinds"); tag != nil {
		supportedKinds = make([]int, 0, len(tag)-1)
		for _, raw := range tag[1:] {
			kind, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("invalid supported kind %q: %w", raw, err)
			}
			supportedKinds = append(supportedKinds, kind)
		}
	}

	// the event carries the whole metadata, so every field is reset first
	group.LastMetadataUpdate = evt.CreatedAt
	group.Name = group.Address.ID
	group.About = ""
	group.Picture = ""
	group.Banner = ""
	group.Private = findTag(evt.Tags, "private") != nil
	group.Closed = findTag(evt.Tags, "closed") != nil
	group.Restricted = findTag(evt.Tags, "restricted") != nil
	group.Hidden = findTag(evt.Tags, "hidden") != nil
	group.SupportedKinds = supportedKinds

	if tag := evt.Tags.Find("name"); tag != nil {
		group.Name = tag[1]
	}
	if tag := evt.Tags.Find("about"); tag != nil {
		group.About = tag[1]
	}
	if tag := evt.Tags.Find("picture"); tag != nil {
		group.Picture = tag[1]
	}
	if tag := evt.Tags.Find("banner"); tag != nil {
		group.Banner = tag[1]
	}

	return nil
}

func (group *Group) MergeInAdminsEvent(evt *nostr.Event) error {
	if evt.Kind != nostr.KindSimpleGroupAdmins {
		return fmt.Errorf("expected kind %d, got %d", nostr.KindSimpleGroupAdmins, evt.Kind)
	}
	if evt.CreatedAt < group.LastAdminsUpdate {
		return fmt.Errorf("event is older than our last update (%d vs %d)", evt.CreatedAt, group.LastAdminsUpdate)
	}

	// the event lists every admin, so an admin it omits holds no role
	group.LastAdminsUpdate = evt.CreatedAt
	for member := range group.Members {
		group.Members[member] = nil
	}
	for _, tag := range evt.Tags {
		if len(tag) < 3 {
			continue
		}
		if tag[0] != "p" {
			continue
		}
		if !nostr.IsValid32ByteHex(tag[1]) {
			continue
		}

		roles := make([]*Role, 0, len(tag)-2)
		for _, roleName := range tag[2:] {
			if slices.ContainsFunc(roles, func(role *Role) bool { return role.Name == roleName }) {
				continue
			}
			roles = append(roles, group.GetRoleByName(roleName))
		}
		group.Members[tag[1]] = roles
	}

	return nil
}

func (group *Group) MergeInMembersEvent(evt *nostr.Event) error {
	if evt.Kind != nostr.KindSimpleGroupMembers {
		return fmt.Errorf("expected kind %d, got %d", nostr.KindSimpleGroupMembers, evt.Kind)
	}
	if evt.CreatedAt < group.LastMembersUpdate {
		return fmt.Errorf("event is older than our last update (%d vs %d)", evt.CreatedAt, group.LastMembersUpdate)
	}

	// the event lists every member, so a member it omits is removed; a member
	// it keeps keeps their roles
	group.LastMembersUpdate = evt.CreatedAt
	members := make(map[string][]*Role, len(evt.Tags))
	for _, tag := range evt.Tags {
		if len(tag) < 2 {
			continue
		}
		if tag[0] != "p" {
			continue
		}
		if !nostr.IsValid32ByteHex(tag[1]) {
			continue
		}

		members[tag[1]] = group.Members[tag[1]]
	}
	group.Members = members

	return nil
}

func (group *Group) MergeInPinsEvent(evt *nostr.Event) error {
	if evt.Kind != nostr.KindSimpleGroupPinnedEvents {
		return fmt.Errorf("expected kind %d, got %d", nostr.KindSimpleGroupPinnedEvents, evt.Kind)
	}
	if evt.CreatedAt < group.LastPinsUpdate {
		return fmt.Errorf("event is older than our last update (%d vs %d)", evt.CreatedAt, group.LastPinsUpdate)
	}

	// the event carries the whole list in display order
	group.LastPinsUpdate = evt.CreatedAt
	pins := make(nostr.Tags, 0, len(evt.Tags))
	for _, tag := range evt.Tags {
		if len(tag) < 2 {
			continue
		}
		if tag[0] != "e" && tag[0] != "a" {
			continue
		}
		pins = append(pins, slices.Clone(tag))
	}
	group.Pins = pins

	return nil
}
