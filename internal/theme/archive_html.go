package theme

import (
	"bytes"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

const maxHTMLNodes = 10000

type htmlAssets struct {
	entry string
	files map[string][]byte
}

func validateHTML(entry string, files map[string][]byte) error {
	root, err := html.Parse(bytes.NewReader(files[entry]))
	if err != nil {
		return ErrArchiveInvalid
	}
	queue := []*html.Node{root}
	visited := 0
	for len(queue) > 0 {
		node := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		visited++
		if visited > maxHTMLNodes {
			return ErrArchiveInvalid
		}
		if err := validateHTMLNode(node, entry, files); err != nil {
			return err
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			queue = append(queue, child)
		}
	}
	return nil
}

func validateHTMLNode(node *html.Node, entry string, files map[string][]byte) error {
	if node.Type != html.ElementNode {
		return nil
	}
	switch node.Data {
	case "base", "form", "iframe", "frame", "frameset", "object", "embed", "svg", "math":
		return ErrArchiveInvalid
	case "script":
		if !hasScriptSource(node) {
			return ErrArchiveInvalid
		}
	}
	for _, attribute := range node.Attr {
		if err := validateHTMLAttribute(node.Data, attribute, htmlAssets{entry: entry, files: files}); err != nil {
			return err
		}
	}
	return nil
}

func hasScriptSource(node *html.Node) bool {
	hasSource := false
	for _, attribute := range node.Attr {
		if attribute.Key == "src" && attribute.Val != "" {
			hasSource = true
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if strings.TrimSpace(child.Data) != "" {
			return false
		}
	}
	return hasSource
}

func validateHTMLAttribute(tag string, attribute html.Attribute, assets htmlAssets) error {
	key := strings.ToLower(attribute.Key)
	if strings.HasPrefix(key, "on") || key == "srcset" || key == "formaction" {
		return ErrArchiveInvalid
	}
	if tag == "meta" && key == "http-equiv" {
		return ErrArchiveInvalid
	}
	if key != "src" && key != "href" && key != "poster" {
		return nil
	}
	if tag == "a" && key == "href" {
		if strings.HasPrefix(attribute.Val, "#") {
			return nil
		}
		return ErrArchiveInvalid
	}
	return validateResourceURL(assets.entry, attribute.Val, assets.files)
}

func validateResourceURL(entry, value string, files map[string][]byte) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return ErrArchiveInvalid
	}
	if parsed.Path == "" || strings.ContainsRune(parsed.Path, '\\') || strings.HasPrefix(parsed.Path, "/") {
		return ErrArchiveInvalid
	}
	resolved := path.Join(path.Dir(entry), parsed.Path)
	if _, err := validateEntryPath(resolved); err != nil {
		return err
	}
	if _, ok := files[resolved]; !ok {
		return ErrArchiveInvalid
	}
	return nil
}
