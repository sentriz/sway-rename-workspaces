package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshuarubin/go-sway"
)

func main() {
	swayClient, err := sway.New(context.Background())
	if err != nil {
		log.Fatalf("error creating client: %v", err)
	}
	clint := &client{
		Client: swayClient,
	}

	handlr := &handler{
		EventHandler: sway.NoOpEventHandler(),
		client:       clint,
		timer:        time.NewTimer(0),
	}

	go handlr.waitUpdateWorkspaceLabels(context.Background())

	events := []sway.EventType{
		sway.EventTypeWorkspace,
		sway.EventTypeWindow,
	}
	if err := sway.Subscribe(context.Background(), handlr, events...); err != nil {
		log.Fatalf("error in subscribe: %v", err)
	}
}

type handler struct {
	sway.EventHandler
	client *client
	timer  *time.Timer
}

const cooldown = 100 * time.Millisecond

func (h *handler) Workspace(ctx context.Context, _ sway.WorkspaceEvent) {
	h.timer.Reset(cooldown)
}
func (h *handler) Window(ctx context.Context, _ sway.WindowEvent) {
	h.timer.Reset(cooldown)
}
func (h *handler) waitUpdateWorkspaceLabels(ctx context.Context) {
	for range h.timer.C {
		if err := h.client.updateWorkspaceLabels(ctx); err != nil {
			log.Printf("error updating workspace labels: %v", err)
		}
	}
}

type client struct {
	sway.Client
}

var matchWorkspace = regexp.MustCompile(`^[0-9]+`)

func (c *client) updateWorkspaceLabels(ctx context.Context) error {
	root, err := c.GetTree(ctx)
	if err != nil {
		return fmt.Errorf("get tree: %w", err)
	}

	return iterWorkspaces(root, func(workspace *sway.Node) error {
		workspaceN, _ := strconv.Atoi(matchWorkspace.FindString(workspace.Name))
		if workspaceN < 1 {
			return nil
		}

		var applicationNames []string
		for _, node := range findApplications(workspace) {
			applicationNames = append(applicationNames, formatName(applicationName(node)))
		}

		workspaceName := fmt.Sprintf("%d", workspaceN)
		if len(applicationNames) > 0 {
			applicationNames = uniqueStable(applicationNames)
			workspaceName = fmt.Sprintf("%d %s", workspaceN, strings.Join(applicationNames, " "))
		}
		if workspaceName == workspace.Name {
			return nil
		}

		command := fmt.Sprintf(`rename workspace number %d to %q`, workspaceN, workspaceName)
		if _, err := c.RunCommand(ctx, command); err != nil {
			return fmt.Errorf("run rename command: %w", err)
		}

		return nil
	})
}

func iterWorkspaces(root *sway.Node, cb func(*sway.Node) error) error {
	for _, output := range root.Nodes {
		for _, workspace := range output.Nodes {
			if workspace.Type != sway.NodeWorkspace {
				continue
			}
			if err := cb(workspace); err != nil {
				return err
			}
		}
	}
	return nil
}

// recurse into node finding any other nodes that have a PID.
// guess the application by looking at the wayland app ID or x11 window class
func findApplications(node *sway.Node) []*sway.Node {
	var nodes []*sway.Node
	if node.PID != nil {
		nodes = append(nodes, node)
	}
	for _, node := range node.Nodes {
		nodes = append(nodes, findApplications(node)...)
	}
	for _, node := range node.FloatingNodes {
		if floatingNodes := findApplications(node); len(floatingNodes) > 0 {
			nodes = append(nodes, floatingNodes[0])
		}
	}
	return nodes
}

func applicationName(node *sway.Node) string {
	if node.AppID != nil {
		return *node.AppID
	}
	if node.WindowProperties != nil && node.WindowProperties.Class != "" {
		return node.WindowProperties.Class
	}
	return ""
}

var (
	matchFQN                  = regexp.MustCompile(`([a-z0-9]+\.)+`)
	matchNumberDisambiguation = regexp.MustCompile(`[0-9.\-_/\|]+($|\s)`)
	matchTrailingParen        = regexp.MustCompile(`\s*[[({].*`)
	matchNonAlphaNum          = regexp.MustCompile(`[^a-z0-9]`)
)

func formatName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = matchFQN.ReplaceAllString(name, "")                  // com.example.xxx -> xxx
	name = matchNumberDisambiguation.ReplaceAllString(name, "") // xxx.123         -> xxx
	name = matchTrailingParen.ReplaceAllString(name, "")        // xxx (yyy)       -> xxx
	name = matchNonAlphaNum.ReplaceAllString(name, " ")         // x-y             -> x y
	name = strings.Join(strings.Fields(name), " ")
	name = strings.TrimSpace(name)
	return name
}

func uniqueStable[T comparable](items []T) []T {
	var out []T
	seen := map[T]struct{}{}
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		out = append(out, item)
		seen[item] = struct{}{}
	}
	return out
}
