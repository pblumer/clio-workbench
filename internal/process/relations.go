// Package process — relations.go derives entity relationships from the subject
// hierarchy of real events.
//
// In Clio, 1:n containment is expressed through the subject path
// (/orders/{id}/items/{id}). We collapse instance ids (numbers, UUIDs, long
// hex) into a single "{id}" node so the structure reads as a schema: a
// collection has N instances (1:n), and each instance can contain further
// collections.
package process

import (
	"regexp"
	"sort"
	"strings"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isID reports whether a path segment looks like an instance identifier rather
// than a named collection.
func isID(s string) bool {
	if s == "" {
		return false
	}
	if uuidRe.MatchString(s) {
		return true
	}
	digits, hex := true, true
	for _, r := range s {
		if r < '0' || r > '9' {
			digits = false
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			hex = false
		}
	}
	if digits {
		return true
	}
	return hex && len(s) >= 16
}

// RelNode is a node in the subject-relationship tree. IsID marks an instance
// level (the "n" of a 1:n); Instances is how many distinct instances occur
// there; Events is how many events fall under the node.
type RelNode struct {
	Seg       string
	IsID      bool
	Events    int
	Instances int
	Children  []*RelNode
}

type relBuild struct {
	seg   string
	isID  bool
	event int
	ids   map[string]struct{}
	kids  map[string]*relBuild
	order []string
}

func newRelBuild(seg string, id bool) *relBuild {
	return &relBuild{seg: seg, isID: id, ids: map[string]struct{}{}, kids: map[string]*relBuild{}}
}

// BuildSubjectTree builds the relationship tree from event subjects (one entry
// per event, so Events reflects volume). The returned root is synthetic.
func BuildSubjectTree(subjects []string) *RelNode {
	root := newRelBuild("", false)
	for _, sub := range subjects {
		cur := root
		for _, seg := range strings.Split(strings.Trim(sub, "/"), "/") {
			if seg == "" {
				continue
			}
			id := isID(seg)
			key := seg
			if id {
				key = "{id}"
			}
			ch := cur.kids[key]
			if ch == nil {
				ch = newRelBuild(key, id)
				cur.kids[key] = ch
				cur.order = append(cur.order, key)
			}
			ch.event++
			if id {
				ch.ids[seg] = struct{}{}
			}
			cur = ch
		}
	}
	return convertRel(root)
}

func convertRel(b *relBuild) *RelNode {
	n := &RelNode{Seg: b.seg, IsID: b.isID, Events: b.event, Instances: len(b.ids)}
	for _, key := range b.order {
		n.Children = append(n.Children, convertRel(b.kids[key]))
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, c := n.Children[i], n.Children[j]
		if a.Events != c.Events {
			return a.Events > c.Events
		}
		return a.Seg < c.Seg
	})
	return n
}
