package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/src-d/go-billy.v4/osfs"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

var (
	refs    = make(map[string]*plumbing.Reference)
	tags    = make(map[plumbing.Hash]bool)
	commits = make(map[plumbing.Hash]bool)
	trees   = make(map[plumbing.Hash]bool)
	blobs   = make(map[plumbing.Hash]bool)
	edges   = make(map[plumbing.Hash][]plumbing.Hash)
)

type options struct {
	noColor bool
	noTypes bool
}

func main() {
	opts := &options{}
	flag.BoolVar(&opts.noColor, "no-color", false, "suppress filling graph nodes with color")
	flag.BoolVar(&opts.noTypes, "no-types", false, "suppress labeling graph nodes with git object types")
	flag.Parse()

	r, err := repo()
	check(err)

	if flag.NArg() > 0 {
		for _, n := range flag.Args() {
			ref, err := r.Reference(plumbing.ReferenceName(n), false)
			if err != nil {
				// Try decoding argument as hash
				h := plumbing.NewHash(n)
				if err := r.Storer.HasEncodedObject(h); err == nil {
					check(walk(r.Storer, h))
					continue
				}
				check(err)
			}
			check(walkRef(r.Storer, ref))
		}
	} else {
		objs, err := r.Storer.IterEncodedObjects(plumbing.AnyObject)
		check(err)
		check(objs.ForEach(func(obj plumbing.EncodedObject) error {
			return walkObj(r.Storer, obj)
		}))
		refs, err := r.References()
		check(err)
		check(refs.ForEach(func(ref *plumbing.Reference) error {
			return walkRef(r.Storer, ref)
		}))
	}

	render(opts)
}

func repo() (*git.Repository, error) {
	if gitdir, ok := os.LookupEnv("GIT_DIR"); ok {
		dotgit, err := filesystem.NewStorage(osfs.New(gitdir))
		if err != nil {
			return nil, err
		}
		return git.Open(dotgit, osfs.New(os.Getenv("GIT_WORK_TREE")))
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return git.PlainOpen(dir)
}

func check(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graphviz: Error: %v\n", err)
		os.Exit(1)
	}
}

func walkRef(s storer.Storer, ref *plumbing.Reference) error {
	name := string(ref.Name())
	if _, ok := refs[name]; ok {
		return nil
	}
	refs[name] = ref
	if ref.Type() == plumbing.HashReference {
		return walk(s, ref.Hash())
	}
	target, err := s.Reference(ref.Target())
	if err != nil {
		return fmt.Errorf("walkRef %s: %v", name, err)
	}
	return walkRef(s, target)
}

func walk(s storer.EncodedObjectStorer, h plumbing.Hash) error {
	for _, seen := range []map[plumbing.Hash]bool{tags, commits, trees, blobs} {
		if seen[h] {
			return nil
		}
	}
	obj, err := s.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return fmt.Errorf("walk %s: %v", h, err)
	}
	return walkObj(s, obj)
}

func walkObj(s storer.EncodedObjectStorer, obj plumbing.EncodedObject) error {
	h := obj.Hash()
	switch obj.Type() {
	case plumbing.TagObject:
		return walkTag(s, h)
	case plumbing.CommitObject:
		return walkCommit(s, h)
	case plumbing.TreeObject:
		return walkTree(s, h)
	case plumbing.BlobObject:
		blobs[h] = true
	}
	return nil
}

func walkTag(s storer.EncodedObjectStorer, h plumbing.Hash) error {
	if tags[h] {
		return nil
	}
	tags[h] = true
	tag, err := object.GetTag(s, h)
	if err != nil {
		return fmt.Errorf("walkTag %s: %v", h, err)
	}
	edges[h] = []plumbing.Hash{tag.Target}
	return walk(s, tag.Target)
}

func walkCommit(s storer.EncodedObjectStorer, h plumbing.Hash) error {
	if commits[h] {
		return nil
	}
	commits[h] = true
	commit, err := object.GetCommit(s, h)
	if err != nil {
		return fmt.Errorf("walkCommit %s: %v", h, err)
	}
	edges[h] = append(commit.ParentHashes[:], commit.TreeHash)
	if err := walkTree(s, commit.TreeHash); err != nil {
		return err
	}
	for _, p := range commit.ParentHashes {
		if err := walkCommit(s, p); err != nil {
			return err
		}
	}
	return nil
}

func walkTree(s storer.EncodedObjectStorer, h plumbing.Hash) error {
	if trees[h] {
		return nil
	}
	trees[h] = true
	t, err := object.GetTree(s, h)
	if err != nil {
		return fmt.Errorf("walkTree %s: %v", h, err)
	}
	for _, entry := range t.Entries {
		if entry.Mode == filemode.Dir {
			edges[h] = append(edges[h], entry.Hash)
			if err := walkTree(s, entry.Hash); err != nil {
				return err
			}
		}
		if entry.Mode.IsFile() {
			edges[h] = append(edges[h], entry.Hash)
			blobs[entry.Hash] = true
		}
		if entry.Mode == filemode.Submodule {
			edges[h] = append(edges[h], entry.Hash)
			if err := walkCommit(s, entry.Hash); err != nil {
				return err
			}
		}
	}
	return nil
}

func render(opts *options) {
	fmt.Println("digraph {")
	nodeAttrs := map[string]string{"fontname": "AnonymousPro"}
	if !opts.noColor {
		nodeAttrs["style"] = "filled"
	}
	fmt.Printf("\tnode %s;\n", renderAttrs(nodeAttrs))
	for h := range tags {
		attrs := map[string]string{
			"label": label(h, "tag", opts.noTypes),
		}
		if !opts.noColor {
			attrs["color"] = "lightskyblue"
		}
		fmt.Printf("\t\"%s\" %s;\n", h, renderAttrs(attrs))
	}
	for h := range commits {
		attrs := map[string]string{
			"group": "commits",
			"label": label(h, "commit", opts.noTypes),
		}
		if !opts.noColor {
			attrs["color"] = "yellowgreen"
		}
		fmt.Printf("\t\"%s\" %s;\n", h, renderAttrs(attrs))
	}
	for h := range trees {
		attrs := map[string]string{
			"label": label(h, "tree", opts.noTypes),
		}
		if !opts.noColor {
			attrs["color"] = "tomato"
		}
		fmt.Printf("\t\"%s\" %s;\n", h, renderAttrs(attrs))
	}
	for h := range blobs {
		attrs := map[string]string{
			"label": label(h, "blob", opts.noTypes),
		}
		if !opts.noColor {
			attrs["color"] = "gold"
		}
		fmt.Printf("\t\"%s\" %s;\n", h, renderAttrs(attrs))
	}
	for name, ref := range refs {
		target := string(ref.Target())
		if ref.Type() == plumbing.HashReference {
			target = ref.Hash().String()
		}
		attrs := map[string]string{"shape": "box"}
		if !opts.noColor {
			attrs["color"] = "plum"
		}
		fmt.Printf("\t\"%s\" %s;\n", name, renderAttrs(attrs))
		fmt.Printf("\t\"%s\" -> \"%s\";\n", name, target)
	}
	for h, targets := range edges {
		for _, target := range targets {
			fmt.Printf("\t\"%s\" -> \"%s\";\n", h, target)
		}
	}
	fmt.Println("}")
}

func renderAttrs(attrs map[string]string) string {
	var as []string
	for k, v := range attrs {
		as = append(as, fmt.Sprintf("%s=\"%s\"", k, v))
	}
	return fmt.Sprintf("[%s]", strings.Join(as, ","))
}

func label(h plumbing.Hash, t string, noTypes bool) string {
	if noTypes {
		return abbrev(h)
	}
	return t + "\\n" + abbrev(h)
}

func abbrev(h plumbing.Hash) string {
	return h.String()[:6]
}
