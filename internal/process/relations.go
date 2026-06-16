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
// than a named collection. Covers numbers, UUIDs, long hex, and long
// alphanumeric ids with a digit (ULID/nanoid-style).
func isID(s string) bool {
	if s == "" {
		return false
	}
	if uuidRe.MatchString(s) {
		return true
	}
	allDigits, allHex, allAlnum, hasDigit := true, true, true, false
	idLike, hasSep := true, false // idLike: only [A-Za-z0-9_-]
	for _, r := range s {
		d := r >= '0' && r <= '9'
		hexCh := d || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		alnum := d || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		sep := r == '-' || r == '_'
		if d {
			hasDigit = true
		} else {
			allDigits = false
		}
		if !hexCh {
			allHex = false
		}
		if !alnum {
			allAlnum = false
		}
		if sep {
			hasSep = true
		}
		if !(alnum || sep) {
			idLike = false
		}
	}
	switch {
	case allDigits:
		return true
	case allHex && len(s) >= 16:
		return true
	case allAlnum && hasDigit && len(s) >= 12:
		return true
	case idLike && hasSep && hasDigit && len(s) >= 4:
		// prefixed ids like EMP-30000, order_123
		return true
	default:
		return false
	}
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
