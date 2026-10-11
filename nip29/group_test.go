package nip29

import (
	"slices"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/require"
)

// newPubKey returns the hex public key of a freshly generated keypair.
func newPubKey(t *testing.T) string {
	t.Helper()
	pk, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	require.NoError(t, err)
	return pk
}

func newTestGroup(t *testing.T) Group {
	t.Helper()
	group, err := NewGroup("relay.example.com'test")
	require.NoError(t, err)
	return group
}

func TestMetadataFlagsRoundTrip(t *testing.T) {
	flags := []struct {
		name string
		set  func(*Group)
		get  func(Group) bool
	}{
		{"private", func(g *Group) { g.Private = true }, func(g Group) bool { return g.Private }},
		{"closed", func(g *Group) { g.Closed = true }, func(g Group) bool { return g.Closed }},
		{"restricted", func(g *Group) { g.Restricted = true }, func(g Group) bool { return g.Restricted }},
		{"hidden", func(g *Group) { g.Hidden = true }, func(g Group) bool { return g.Hidden }},
	}

	for _, flag := range flags {
		for _, set := range []bool{true, false} {
			source := newTestGroup(t)
			if set {
				flag.set(&source)
			}
			evt := source.ToMetadataEvent()

			require.Nil(t, findTag(evt.Tags, "public"), "public is not a NIP-29 tag")
			require.Nil(t, findTag(evt.Tags, "open"), "open is not a NIP-29 tag")
			require.Equal(t, set, findTag(evt.Tags, flag.name) != nil, "flag %s set=%v", flag.name, set)

			target := newTestGroup(t)
			require.NoError(t, target.MergeInMetadataEvent(evt))
			require.Equal(t, set, flag.get(target), "flag %s set=%v after merge", flag.name, set)
			for _, other := range flags {
				if other.name != flag.name {
					require.False(t, other.get(target), "flag %s leaked from %s", other.name, flag.name)
				}
			}
		}
	}
}

func TestMetadataMergeResetsOmittedFields(t *testing.T) {
	group := newTestGroup(t)

	full := newTestGroup(t)
	full.Name = "full"
	full.About = "about"
	full.Picture = "https://example.com/p.png"
	full.Banner = "https://example.com/b.png"
	full.Private = true
	full.Closed = true
	full.Restricted = true
	full.Hidden = true
	full.SupportedKinds = []int{9}
	full.LastMetadataUpdate = 100
	require.NoError(t, group.MergeInMetadataEvent(full.ToMetadataEvent()))
	require.Equal(t, "https://example.com/b.png", group.Banner)
	require.True(t, group.Hidden)

	bare := newTestGroup(t)
	bare.LastMetadataUpdate = 200
	require.NoError(t, group.MergeInMetadataEvent(bare.ToMetadataEvent()))

	require.Equal(t, "test", group.Name, "name falls back to the group id")
	require.Empty(t, group.About)
	require.Empty(t, group.Picture)
	require.Empty(t, group.Banner)
	require.False(t, group.Private)
	require.False(t, group.Closed)
	require.False(t, group.Restricted)
	require.False(t, group.Hidden)
	require.Nil(t, group.SupportedKinds)
}

func TestSupportedKinds(t *testing.T) {
	cases := []struct {
		name  string
		kinds []int
	}{
		{"absent means every kind", nil},
		{"empty means no kind", []int{}},
		{"listed kinds", []int{9, 11, 30818}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := newTestGroup(t)
			source.SupportedKinds = tc.kinds
			evt := source.ToMetadataEvent()

			target := newTestGroup(t)
			require.NoError(t, target.MergeInMetadataEvent(evt))
			if tc.kinds == nil {
				require.Nil(t, target.SupportedKinds)
				return
			}
			require.NotNil(t, target.SupportedKinds)
			require.Equal(t, tc.kinds, target.SupportedKinds)
		})
	}
}

func TestSupportedKindsInvalidLeavesGroupUnchanged(t *testing.T) {
	group := newTestGroup(t)
	group.Name = "before"
	group.Private = true

	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: 10,
		Tags:      nostr.Tags{{"d", "test"}, {"name", "after"}, {"supported_kinds", "9", "nine"}},
	}
	require.Error(t, group.MergeInMetadataEvent(evt))
	require.Equal(t, "before", group.Name)
	require.True(t, group.Private)
	require.Equal(t, nostr.Timestamp(0), group.LastMetadataUpdate)
}

func TestRolesEventUsesRolesTimestamp(t *testing.T) {
	group := newTestGroup(t)
	group.Roles = []*Role{{Name: "admin", Description: "every action"}, {Name: "moderator"}}
	group.LastMembersUpdate = 5
	group.LastRolesUpdate = 7

	evt := group.ToRolesEvent()
	require.Equal(t, nostr.Timestamp(7), evt.CreatedAt)
	require.Equal(t, nostr.Tags{{"d", "test"}, {"role", "admin", "every action"}, {"role", "moderator", ""}}, evt.Tags)
}

func TestAdminsEventReplacesRoles(t *testing.T) {
	alice, bob, carol := newPubKey(t), newPubKey(t), newPubKey(t)

	group := newTestGroup(t)
	group.Members[alice] = []*Role{{Name: "admin"}}
	group.Members[bob] = []*Role{{Name: "admin"}, {Name: "moderator"}}
	group.Members[carol] = nil

	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupAdmins,
		CreatedAt: 10,
		Tags:      nostr.Tags{{"d", "test"}, {"p", alice, "moderator", "moderator"}},
	}
	require.NoError(t, group.MergeInAdminsEvent(evt))

	require.Len(t, group.Members, 3, "an admins event never removes a member")
	require.Len(t, group.Members[alice], 1, "a repeated role is held once")
	require.Equal(t, "moderator", group.Members[alice][0].Name, "the listed roles replace the old ones")
	require.Empty(t, group.Members[bob], "an admin the event omits holds no role")
	require.Empty(t, group.Members[carol])
}

func TestMembersEventReplacesMembers(t *testing.T) {
	alice, bob, carol := newPubKey(t), newPubKey(t), newPubKey(t)

	group := newTestGroup(t)
	group.Members[alice] = []*Role{{Name: "admin"}}
	group.Members[bob] = nil
	group.Members[carol] = nil

	evt := &nostr.Event{
		Kind:      nostr.KindSimpleGroupMembers,
		CreatedAt: 10,
		Tags:      nostr.Tags{{"d", "test"}, {"p", alice}, {"p", bob}},
	}
	require.NoError(t, group.MergeInMembersEvent(evt))

	require.Len(t, group.Members, 2)
	require.NotContains(t, group.Members, carol, "a member the event omits is removed")
	require.Equal(t, "admin", group.Members[alice][0].Name, "a kept member keeps their roles")
}

func TestListEventsSortTheirMembers(t *testing.T) {
	group := newTestGroup(t)
	var keys []string
	for range 12 {
		pk := newPubKey(t)
		keys = append(keys, pk)
		group.Members[pk] = []*Role{{Name: "admin"}}
	}
	slices.Sort(keys)

	for _, evt := range []*nostr.Event{group.ToMembersEvent(), group.ToAdminsEvent()} {
		var listed []string
		for _, tag := range evt.Tags[1:] {
			listed = append(listed, tag[1])
		}
		require.Equal(t, keys, listed, "kind %d", evt.Kind)
		require.Equal(t, evt.Tags, func() nostr.Tags {
			if evt.Kind == nostr.KindSimpleGroupMembers {
				return group.ToMembersEvent().Tags
			}
			return group.ToAdminsEvent().Tags
		}(), "kind %d is the same on every call", evt.Kind)
	}
}

func TestEqualTimestampStillApplies(t *testing.T) {
	alice, bob := newPubKey(t), newPubKey(t)
	group := newTestGroup(t)

	first := &nostr.Event{Kind: nostr.KindSimpleGroupMembers, CreatedAt: 50, Tags: nostr.Tags{{"d", "test"}, {"p", alice}}}
	second := &nostr.Event{Kind: nostr.KindSimpleGroupMembers, CreatedAt: 50, Tags: nostr.Tags{{"d", "test"}, {"p", alice}, {"p", bob}}}
	older := &nostr.Event{Kind: nostr.KindSimpleGroupMembers, CreatedAt: 49, Tags: nostr.Tags{{"d", "test"}}}

	require.NoError(t, group.MergeInMembersEvent(first))
	require.NoError(t, group.MergeInMembersEvent(second))
	require.Len(t, group.Members, 2, "a list with an equal timestamp applies")
	require.Error(t, group.MergeInMembersEvent(older))
	require.Len(t, group.Members, 2, "an older list does not apply")
}

func TestPinsRoundTrip(t *testing.T) {
	author := newPubKey(t)
	group := newTestGroup(t)
	group.Pins = nostr.Tags{
		{"a", "30818:" + author + ":welcome"},
		{"e", "5c83da77af1dec6d7289834998ad7aafbd9e2191396d75ec3cc27f5a77226f36"},
	}
	group.LastPinsUpdate = 9

	evt := group.ToPinsEvent()
	require.Equal(t, nostr.KindSimpleGroupPinnedEvents, evt.Kind)
	require.Equal(t, nostr.Timestamp(9), evt.CreatedAt)

	target := newTestGroup(t)
	target.Pins = nostr.Tags{{"e", "stale"}}
	evt.Tags = append(evt.Tags, nostr.Tag{"p", author})
	require.NoError(t, target.MergeInPinsEvent(evt))
	require.Equal(t, group.Pins, target.Pins, "the list is replaced, in order, without other tags")
}

func TestKindRangesAreSorted(t *testing.T) {
	require.True(t, slices.IsSorted(ModerationEventKinds))
	require.True(t, slices.IsSorted(MetadataEventKinds))
	require.True(t, ModerationEventKinds.Includes(nostr.KindSimpleGroupUpdatePinList))
	require.True(t, MetadataEventKinds.Includes(nostr.KindSimpleGroupPinnedEvents))
}
