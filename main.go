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

	// walk commits from HEAD
	commitIter, err := r.Log(&git.LogOptions{})
	check(err)
	check(commitIter.ForEach(func(commit *object.Commit) error {
		commits[commit.ID()] = true
		edges[commit.ID()] = append(commit.ParentHashes[:], commit.TreeHash)
		return walkTree(r.Storer, commit.TreeHash)
	}))

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
		fmt.Fprintf(os.Stderr, "git-graphviz: Error: %s", err)
		os.Exit(1)
	}
}

func walkTree(s storer.EncodedObjectStorer, h plumbing.Hash) error {
	if trees[h] {
		return nil
	}
	trees[h] = true
	t, err := object.GetTree(s, h)
	if err != nil {
		return err
	}
	for _, entry := range t.Entries {
		if entry.Mode == filemode.Dir {
			err = walkTree(s, entry.Hash)
			if err != nil {
				return err
			}
			edges[h] = append(edges[h], entry.Hash)
		}
		if entry.Mode.IsFile() {
			blobs[entry.Hash] = true
			edges[h] = append(edges[h], entry.Hash)
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
