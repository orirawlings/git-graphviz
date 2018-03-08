package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

var (
	commits = make(map[plumbing.Hash]bool)
	trees   = make(map[plumbing.Hash]bool)
	blobs   = make(map[plumbing.Hash]bool)
	edges   = make(map[plumbing.Hash][]plumbing.Hash)
)

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

func abbrev(h plumbing.Hash) string {
	return h.String()[:6]
}

func main() {
	cwd, err := os.Getwd()
	check(err)
	r, err := git.PlainOpen(cwd)
	check(err)

	// walk commits from HEAD
	commitIter, err := r.Log(&git.LogOptions{})
	check(err)
	check(commitIter.ForEach(func(commit *object.Commit) error {
		commits[commit.ID()] = true
		edges[commit.ID()] = append(commit.ParentHashes[:], commit.TreeHash)
		return walkTree(r.Storer, commit.TreeHash)
	}))

	fmt.Println("digraph {\n\tnode [fontname=AnonymousPro,style=filled];")
	for h := range commits {
		fmt.Printf("\t\"%s\" [label=\"commit\\n%s\",color=yellowgreen,group=commits];\n", h, abbrev(h))
	}
	for h := range trees {
		fmt.Printf("\t\"%s\" [label=\"tree\\n%s\",color=tomato];\n", h, abbrev(h))
	}
	for h := range blobs {
		fmt.Printf("\t\"%s\" [label=\"blob\\n%s\",color=gold];\n", h, abbrev(h))
	}
	for h, targets := range edges {
		for _, target := range targets {
			fmt.Printf("\t\"%s\" -> \"%s\";\n", h, target)
		}
	}
	fmt.Println("}")
}
