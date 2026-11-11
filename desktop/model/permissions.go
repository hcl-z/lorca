package model

import (
	"encoding/json"
	"slices"
)

// ToolPermission is one switch in a bot's Access: on or off, and whether an on tool asks first.
// A tool that asks runs only when the user answers its card, whatever Auto-review says.
type ToolPermission struct {
	Enabled bool `json:"enabled"`
	Ask     bool `json:"ask,omitempty"`
}

var (
	ToolOff = ToolPermission{}
	ToolOn  = ToolPermission{Enabled: true}
	// ToolAsks is on, but every call waits for the user.
	ToolAsks = ToolPermission{Enabled: true, Ask: true}
)

// Level is the three states the Access sheet offers.
func (p ToolPermission) Level() AccessLevel {
	switch {
	case !p.Enabled:
		return AccessNone
	case p.Ask:
		return AccessAsk
	}
	return AccessOn
}

// BotPermissions is a bot's Access, as the CLI keeps it in the encrypted roster. A bot with no
// policy is one made before this model, which has every plugin and every own tool on.
type BotPermissions struct {
	// Connections nil is every plugin on the Runner, one installed later included; a map leaves
	// out the ones it does not list.
	Connections *map[string]ConnectionPermissions `json:"connections,omitempty"`
	Tools       LocalPermissions                  `json:"tools,omitempty"`
	Skills      SkillPermissions                  `json:"skills,omitempty"`
	// Drafts is whether the bot's emails and Slack messages wait in the chat as drafts for the
	// user to send.
	Drafts bool `json:"drafts"`
}

// LocalPermissions are the bot's own tools, each switched.
type LocalPermissions struct {
	Shell ToolPermission `json:"shell"`
	Read  ToolPermission `json:"read"`
	Write ToolPermission `json:"write"`
}

// SkillPermissions are the skills a bot sees: exceptions over the default, which leaves a bot's
// own and a group's off and the shared ones on. A skill Lorca ships always shows.
type SkillPermissions struct {
	Enabled  []string `json:"enabled,omitempty"`
	Disabled []string `json:"disabled,omitempty"`
}

// ConnectionPermissions is one plugin's switch and its tools' own switches. A tool not listed
// takes the plugin's.
type ConnectionPermissions struct {
	Enabled bool                      `json:"enabled"`
	Tools   map[string]ToolPermission `json:"tools,omitempty"`
}

// Tool is the switch for one tool of this plugin.
func (c ConnectionPermissions) Tool(name string) ToolPermission {
	if tool, ok := c.Tools[name]; ok {
		return tool
	}
	return ToolPermission{Enabled: c.Enabled}
}

func (p *BotPermissions) UnmarshalJSON(data []byte) error {
	type plain BotPermissions
	value := plain{Tools: LocalPermissions{Shell: ToolOn, Read: ToolOn, Write: ToolOn}, Drafts: true}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*p = BotPermissions(value)
	return nil
}

// FullAccess is the explicit policy of a bot made before the switches: every plugin and own tool
// on, which the CLI reads as the older model.
func FullAccess() *BotPermissions {
	return &BotPermissions{}
}

// NewBotAccess is what a bot the user just made starts with: every plugin off. Its setup turn
// asks what its job needs and the user turns those on.
func NewBotAccess() *BotPermissions {
	connections := map[string]ConnectionPermissions{}
	return &BotPermissions{
		Connections: &connections,
		Tools:       LocalPermissions{Shell: ToolOn, Read: ToolOn, Write: ToolOn},
		Drafts:      true,
	}
}

// Level is a plugin's switch: on or off. A plugin is never "asking"; that is a tool's own
// setting, shown on each tool in it.
func (p *BotPermissions) Level(pluginID string) AccessLevel {
	if p == nil || p.Connections == nil {
		return AccessOn
	}
	grant, ok := (*p.Connections)[pluginID]
	if !ok || !grant.Enabled {
		return AccessNone
	}
	return AccessOn
}

// ToolLevel is one tool's switch in a plugin: on, asking first, or off.
func (p *BotPermissions) ToolLevel(pluginID, tool string) AccessLevel {
	if p == nil || p.Connections == nil {
		return AccessOn
	}
	grant, ok := (*p.Connections)[pluginID]
	if !ok || !grant.Enabled {
		return AccessNone
	}
	return grant.Tool(tool).Level()
}

// ShowsSkill is whether the bot reads the skill `name` in `scope`. Lorca's own always show, since
// they are how a bot reads its docs and writes one; the shared ones start on and a bot's own and a
// group's start off.
func (p *BotPermissions) ShowsSkill(scope, name string, builtin bool) bool {
	if builtin || p == nil {
		return true
	}
	key := scope + "/" + name
	if slices.Contains(p.Skills.Enabled, key) {
		return true
	}
	if slices.Contains(p.Skills.Disabled, key) {
		return false
	}
	return scope == "shared"
}

// SetSkill records a skill's switch, leaving it to the default when it matches again.
func (p *BotPermissions) SetSkill(scope, name string, on, builtin bool) {
	if builtin {
		return
	}
	key := scope + "/" + name
	byDefault := scope == "shared"
	remove := func(list []string) []string {
		return slices.DeleteFunc(slices.Clone(list), func(each string) bool { return each == key })
	}
	p.Skills.Enabled = remove(p.Skills.Enabled)
	p.Skills.Disabled = remove(p.Skills.Disabled)
	switch {
	case on && !byDefault:
		p.Skills.Enabled = append(p.Skills.Enabled, key)
	case !on && byDefault:
		p.Skills.Disabled = append(p.Skills.Disabled, key)
	}
}

// Summary is the Profile card's word for it.
func (p *BotPermissions) Summary() string {
	if p == nil || (p.Connections == nil && p.Tools.Shell.Enabled && p.Tools.Read.Enabled && p.Tools.Write.Enabled) {
		return L("Full access")
	}
	return L("Limited")
}

func (p *BotPermissions) Clone() *BotPermissions {
	if p == nil {
		return nil
	}
	data, _ := json.Marshal(p)
	var copy BotPermissions
	_ = json.Unmarshal(data, &copy)
	return &copy
}

func EqualBotPermissions(a, b *BotPermissions) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// AccessLevel is a switch: on, asking first, or off. A plugin is only ever on or off; the own
// tools, and each tool in a plugin, may also ask.
type AccessLevel string

const (
	AccessOn   AccessLevel = "on"
	AccessAsk  AccessLevel = "ask"
	AccessNone AccessLevel = "none"
)

// AccessLevels are the levels, the least cautious first. A plugin's pop-up offers On and Off
// only (PluginAccessLevels); a tool and the Runner's own tools offer all three.
var AccessLevels = []AccessLevel{AccessOn, AccessAsk, AccessNone}

// PluginAccessLevels are a plugin's two states.
var PluginAccessLevels = []AccessLevel{AccessOn, AccessNone}

// Permission is the switch an AccessLevel stands for.
func (l AccessLevel) Permission() ToolPermission {
	switch l {
	case AccessOn:
		return ToolOn
	case AccessAsk:
		return ToolAsks
	}
	return ToolOff
}

func (l AccessLevel) Title() string {
	switch l {
	case AccessOn:
		return L("Allowed")
	case AccessAsk:
		return L("Ask first")
	}
	return L("Off")
}

// AccessCatalog is the plugins on a bot's Runner and the tools each offered when it last
// connected, for the Access sheet.
type AccessCatalog struct {
	Connections []AccessPlugin `json:"connections"`
}

type AccessPlugin struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Tools []AccessTool `json:"tools"`
}

type AccessTool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Capability is what the tool does, as its Runner last read it: read, draft, or write. The
	// sheet shows it beside the switch; it decides nothing.
	Capability string `json:"capability"`
}

// Shown is the tool's title, or its name when it has none.
func (t AccessTool) Shown() string {
	if t.Title != "" {
		return t.Title
	}
	return t.Name
}

// BotAccessCatalog asks the bot's Runner for its plugins and their tools.
func (s *Store) BotAccessCatalog(id string, done func(AccessCatalog, error)) {
	if s.IsMock {
		catalog := MockAccessCatalog()
		s.post(func() { done(catalog, nil) })
		return
	}
	Async(s, func() (AccessCatalog, error) {
		return call[AccessCatalog](s, "bots.permissions", map[string]any{"id": id})
	}, done)
}

// SetBotPermissions is only ever the user's: the CLI also dismisses the access requests the bot
// left.
func (s *Store) SetBotPermissions(id string, policy *BotPermissions) {
	bot := s.Bot(id)
	if bot == nil || policy == nil {
		return
	}
	bot.Permissions = policy.Clone()
	s.rosterTouched(id)
	s.perform("bots.update", map[string]any{"id": id, "permissions": policy})
}

// MockAccessCatalog is what Workbench's plugins offered when they last connected.
func MockAccessCatalog() AccessCatalog {
	catalog := AccessCatalog{Connections: []AccessPlugin{
		{ID: "github", Name: "GitHub", Tools: []AccessTool{
			{Name: "search_issues", Title: "Search issues", Capability: "read"},
			{Name: "get_pull_request", Title: "Get a pull request", Capability: "read"},
			{Name: "create_pull_request_review", Title: "Draft a review", Capability: "draft"},
			{Name: "create_issue", Title: "Create an issue", Capability: "write"},
			{Name: "merge_pull_request", Title: "Merge a pull request", Capability: "write"},
		}},
		{ID: "linear", Name: "Linear", Tools: []AccessTool{
			{Name: "list_issues", Title: "List issues", Capability: "read"},
			{Name: "create_issue", Title: "Create an issue", Capability: "write"},
		}},
	}}
	for _, server := range mockMcpServers() {
		if !server.Enabled {
			continue
		}
		plugin := AccessPlugin{ID: server.ID, Name: server.Name}
		for _, tool := range server.Tools {
			capability := "write"
			if tool.ReadOnly {
				capability = "read"
			}
			plugin.Tools = append(plugin.Tools, AccessTool{Name: tool.Name, Description: tool.Description, Capability: capability})
		}
		catalog.Connections = append(catalog.Connections, plugin)
	}
	return catalog
}

// mockWriterAccess lets the Writer read GitHub and its notes folder, and run no commands.
func mockWriterAccess() *BotPermissions {
	connections := map[string]ConnectionPermissions{
		// One tool asks first; the rest of GitHub is on.
		"github": {
			Enabled: true,
			Tools: map[string]ToolPermission{
				"create_pull_request_review": ToolAsks,
				"search_issues":              ToolOn,
			},
		},
		"filesystem": {Enabled: true},
		"deepwiki":   {Enabled: true},
	}
	return &BotPermissions{
		Connections: &connections,
		Tools:       LocalPermissions{Shell: ToolOff, Read: ToolOn, Write: ToolOn},
		Drafts:      true,
	}
}
