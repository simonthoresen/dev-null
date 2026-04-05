package widget

// Widget eval-file harness.
//
// Each file in testdata/*.txt describes a set of render tests for one widget
// type.  The format inside each file is:
//
//	=== test_name
//	widget: <type>
//	title:  <window title>     (optional)
//	width:  <int>              (default 30)
//	height: <int>              (default 5)
//	focused: true|false        (default false)
//	<widget-specific props>
//	===
//	<expected output — ANSI stripped, trailing spaces trimmed per line>
//
//	=== next_test_name
//	...
//
// A line starting with "===" followed by a space and a name starts a new test
// block and also ends the previous one.  A bare "===" line separates the
// config section from the expected output.
//
// Widget-specific props:
//
//	textview  — lines: <text> (repeated), scrollable: bool,
//	            scroll_offset: int, bottom_align: bool
//	listbox   — items: <text> (repeated), tags: <text> (repeated), cursor: int
//	button    — label: <text>
//	label     — text: <text>, align: left|center|right
//	checkbox  — label: <text>, checked: bool
//	hdivider  — connected: bool
//	textinput — value: <text>, placeholder: <text>
//
// Run with -update-widgets to regenerate all expected outputs:
//
//	go test ./internal/widget/ -run TestWidgetHarness -update-widgets

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/domain"
	"null-space/internal/render"
)

var updateWidgets = flag.Bool("update-widgets", false, "regenerate widget eval file golden outputs")

// ─── Data structures ─────────────────────────────────────────────────────────

// widgetCase is one parsed test case from an eval file.
type widgetCase struct {
	name        string
	configRaw   string // raw config text (preserved for -update rewrites)
	props       map[string]string
	lists       map[string][]string
	expected    string
	hasExpected bool // true once the === separator has been seen
}

// ─── Parser ──────────────────────────────────────────────────────────────────

// parseWidgetEvalFile splits an eval file into test cases.
func parseWidgetEvalFile(data string) []widgetCase {
	var cases []widgetCase

	type parseState int
	const (
		stateNone     parseState = iota
		stateConfig              // reading widget config
		stateExpected            // reading expected output
	)

	state := stateNone
	var cur *widgetCase
	var configLines []string
	var expectedLines []string

	flush := func() {
		if cur == nil {
			return
		}
		cur.configRaw = strings.Join(configLines, "\n")
		// Trim trailing blank lines from expected.
		exp := strings.Join(expectedLines, "\n")
		cur.expected = strings.TrimRight(exp, "\n")
		cases = append(cases, *cur)
		cur = nil
		configLines = nil
		expectedLines = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "=== ") {
			// Start of a new test block.
			flush()
			state = stateConfig
			cur = &widgetCase{
				name:  strings.TrimPrefix(line, "=== "),
				props: make(map[string]string),
				lists: make(map[string][]string),
			}
		} else if line == "===" {
			// Separator between config and expected.
			state = stateExpected
			if cur != nil {
				cur.hasExpected = true
			}
		} else if state == stateConfig {
			if strings.HasPrefix(line, "#") {
				// Comment — keep in configRaw but don't parse.
				configLines = append(configLines, line)
				continue
			}
			configLines = append(configLines, line)
			idx := strings.Index(line, ":")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if existing, ok := cur.lists[key]; ok {
				// Already a list key — append.
				cur.lists[key] = append(existing, val)
			} else if _, ok := cur.props[key]; ok {
				// Scalar seen before — upgrade to list.
				cur.lists[key] = []string{cur.props[key], val}
				delete(cur.props, key)
			} else {
				cur.props[key] = val
			}
		} else if state == stateExpected {
			expectedLines = append(expectedLines, line)
		}
	}
	flush()
	return cases
}

// ─── Widget builder ──────────────────────────────────────────────────────────

func propStr(props map[string]string, key, def string) string {
	if v, ok := props[key]; ok {
		return v
	}
	return def
}

func propInt(props map[string]string, key string, def int) int {
	if v, ok := props[key]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return def
}

func propBool(props map[string]string, key string, def bool) bool {
	if v, ok := props[key]; ok {
		return strings.TrimSpace(v) == "true"
	}
	return def
}

func propList(props map[string]string, lists map[string][]string, key string) []string {
	if l, ok := lists[key]; ok {
		return l
	}
	if v, ok := props[key]; ok {
		return []string{v}
	}
	return nil
}

// buildTestWindow creates a Window wrapping the configured widget.
// Returns the Window and its total (width, height).
func buildTestWindow(tc widgetCase) (*Window, int, int) {
	w := propInt(tc.props, "width", 30)
	h := propInt(tc.props, "height", 5)
	title := propStr(tc.props, "title", "")
	focused := propBool(tc.props, "focused", false)
	widgetType := strings.ToLower(strings.TrimSpace(tc.props["widget"]))

	var win *Window
	if widgetType == "junction" {
		win = buildJunctionWindow(tc)
	} else {
		ctrl := buildWidget(tc)
		win = &Window{
			Children: []GridChild{{
				Control:    ctrl,
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth},
			}},
		}
	}
	win.Title = title
	if focused {
		win.FocusFirst()
	}
	return win, w, h
}

// buildJunctionWindow builds a multi-child Window for junction character tests.
// The layout prop selects the specific grid configuration.
func buildJunctionWindow(tc widgetCase) *Window {
	layout := propStr(tc.props, "layout", "")
	lbl := func(text string) GridChild {
		return GridChild{
			Control:    &Label{Text: text, Align: "left"},
			Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth},
		}
	}
	lblAt := func(text string, col, row int) GridChild {
		return GridChild{
			Control:    &Label{Text: text, Align: "left"},
			Constraint: GridConstraint{Col: col, Row: row, WeightX: 1, WeightY: 1, Fill: FillBoth},
		}
	}
	vdiv := func(col, row, rowSpan int, connected bool) GridChild {
		rs := rowSpan
		if rs < 1 {
			rs = 1
		}
		return GridChild{
			Control:    &VDivider{Connected: connected},
			Constraint: GridConstraint{Col: col, Row: row, RowSpan: rs, MinW: 1, WeightY: 1, Fill: FillBoth},
		}
	}
	hdiv := func(col, row, colSpan int, connected bool) GridChild {
		cs := colSpan
		if cs < 1 {
			cs = 1
		}
		return GridChild{
			Control:    &HDivider{Connected: connected},
			Constraint: GridConstraint{Col: col, Row: row, ColSpan: cs, MinH: 1, WeightX: 1, Fill: FillBoth},
		}
	}
	_ = lbl

	switch layout {
	case "vdivider_connected":
		// 3-col: Label | VDivider(Connected) | Label — produces CrossT + CrossB.
		return &Window{Children: []GridChild{
			{Control: &Label{Text: "Left", Align: "left"}, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth}},
			vdiv(1, 0, 1, true),
			{Control: &Label{Text: "Right", Align: "left"}, Constraint: GridConstraint{Col: 2, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth}},
		}}

	case "hdivider_inner_cross":
		// 2-col 3-row: VDivider(Connected) full height at col 0, labels and HDivider at col 1.
		// HDivider at (col=1, row=1) ends at right border → CrossR; starts after VDivider → InnerCrossL.
		// VDivider at (col=0, rows 0-2) → CrossT + CrossB.
		return &Window{Children: []GridChild{
			vdiv(0, 0, 3, true),
			lblAt("Top", 1, 0),
			hdiv(1, 1, 1, true),
			lblAt("Bottom", 1, 2),
		}}

	case "vdivider_inner_cross":
		// 3-col 2-row: HDivider(Connected) full width at row 0, labels and VDivider at row 1.
		// VDivider at (col=1, row=1) starts below HDivider → InnerCrossT; ends at bottom border → CrossB.
		// HDivider at (col=0..2, row=0) → CrossL + CrossR.
		return &Window{Children: []GridChild{
			hdiv(0, 0, 3, true),
			lblAt("Left", 0, 1),
			vdiv(1, 1, 1, true),
			lblAt("Right", 2, 1),
		}}

	case "grid_cross":
		// 3-col 3-row grid: VDivider full height at col 1, HDivider at row 1 in col 0 and col 2.
		// Produces: CrossT + CrossB on VDivider, CrossL on HDivider(col=0), CrossR on HDivider(col=2),
		// InnerCrossR where HDivider(col=0) meets VDivider, InnerCrossL where HDivider(col=2) meets VDivider.
		return &Window{Children: []GridChild{
			lblAt("TL", 0, 0),
			vdiv(1, 0, 3, true),
			lblAt("TR", 2, 0),
			hdiv(0, 1, 1, true),
			hdiv(2, 1, 1, true),
			lblAt("BL", 0, 2),
			lblAt("BR", 2, 2),
		}}

	default:
		panic(fmt.Sprintf("harness: unknown junction layout %q", layout))
	}
}

// buildWidget constructs the Control described by the config props.
func buildWidget(tc widgetCase) Control {
	widgetType := strings.ToLower(strings.TrimSpace(tc.props["widget"]))
	switch widgetType {
	case "textview":
		lines := propList(tc.props, tc.lists, "lines")
		return &TextView{
			Lines:        lines,
			Scrollable:   propBool(tc.props, "scrollable", false),
			ScrollOffset: propInt(tc.props, "scroll_offset", 0),
			BottomAlign:  propBool(tc.props, "bottom_align", false),
		}

	case "listbox":
		items := propList(tc.props, tc.lists, "items")
		tags := propList(tc.props, tc.lists, "tags")
		return &ListBox{
			Items:  items,
			Tags:   tags,
			Cursor: propInt(tc.props, "cursor", 0),
		}

	case "button":
		return &Button{Label: propStr(tc.props, "label", "OK")}

	case "label":
		return &Label{
			Text:  propStr(tc.props, "text", ""),
			Align: propStr(tc.props, "align", "left"),
		}

	case "checkbox":
		return &Checkbox{
			Label:   propStr(tc.props, "label", ""),
			Checked: propBool(tc.props, "checked", false),
		}

	case "hdivider":
		return &HDivider{Connected: propBool(tc.props, "connected", false)}

	case "vdivider":
		return &VDivider{Connected: propBool(tc.props, "connected", false)}

	case "statusbar":
		return &StatusBar{
			LeftText:  propStr(tc.props, "left_text", ""),
			RightText: propStr(tc.props, "right_text", ""),
		}

	case "textinput":
		m := new(textinput.Model)
		*m = textinput.New()
		m.Prompt = ""
		m.Placeholder = propStr(tc.props, "placeholder", "")
		m.CharLimit = 256
		val := propStr(tc.props, "value", "")
		if val != "" {
			m.SetValue(val)
		}
		return &TextInput{Model: m}

	case "textarea":
		lines := propList(tc.props, tc.lists, "lines")
		if len(lines) == 0 {
			lines = []string{""}
		}
		return &TextArea{
			Lines:     lines,
			CursorRow: propInt(tc.props, "cursor_row", 0),
			CursorCol: propInt(tc.props, "cursor_col", 0),
		}

	case "gameview":
		return &GameView{}

	case "table":
		rawRows := propList(tc.props, tc.lists, "row")
		rows := make([][]string, len(rawRows))
		for i, r := range rawRows {
			rows[i] = strings.Split(r, "|")
		}
		return &Table{Rows: rows}

	case "panel":
		childText := propStr(tc.props, "child_text", "")
		return &Panel{
			Title: propStr(tc.props, "panel_title", ""),
			Children: []GridChild{{
				Control:    &Label{Text: childText, Align: "left"},
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth},
			}},
		}

	case "container":
		children := propList(tc.props, tc.lists, "child")
		if len(children) == 0 {
			children = []string{"A", "B"}
		}
		cc := make([]ContainerChild, len(children))
		for i, text := range children {
			cc[i] = ContainerChild{Control: &Label{Text: text, Align: "left"}, Weight: 1}
		}
		return &Container{
			Horizontal: propBool(tc.props, "horizontal", true),
			Children:   cc,
		}

	case "teampanel":
		rawTeams := propList(tc.props, tc.lists, "team")
		var teams []domain.Team
		for _, rt := range rawTeams {
			parts := strings.SplitN(rt, "|", 3)
			name, color := "", "#888888"
			var players []string
			if len(parts) >= 1 {
				name = strings.TrimSpace(parts[0])
			}
			if len(parts) >= 2 {
				color = strings.TrimSpace(parts[1])
			}
			if len(parts) >= 3 {
				for _, p := range strings.Split(parts[2], ",") {
					if p = strings.TrimSpace(p); p != "" {
						players = append(players, p)
					}
				}
			}
			teams = append(teams, domain.Team{Name: name, Color: color, Players: players})
		}
		unassigned := propList(tc.props, tc.lists, "unassigned")
		return &TeamPanel{
			Teams:      teams,
			Unassigned: unassigned,
			MyTeamIdx:  propInt(tc.props, "my_team_idx", -1),
			PlayerID:   propStr(tc.props, "player_id", ""),
			ShowCreate: propBool(tc.props, "show_create", false),
		}

	default:
		panic(fmt.Sprintf("harness: unknown widget type %q", widgetType))
	}
}

// ─── Rendering & normalisation ───────────────────────────────────────────────

// renderCase renders tc and returns the normalised plain-text output.
func renderCase(tc widgetCase, profile colorprofile.Profile) string {
	widgetType := strings.ToLower(strings.TrimSpace(tc.props["widget"]))
	w := propInt(tc.props, "width", 30)
	h := propInt(tc.props, "height", 1)
	buf := render.NewImageBuffer(w, h)
	layer := testLayer()

	// Bare widgets (no Window border) — render the control directly.
	switch widgetType {
	case "statusbar":
		ctrl := buildWidget(tc)
		ctrl.Render(buf, 0, 0, w, h, false, layer)
		return normaliseWidgetOutput(buf.ToString(profile))

	case "menubar":
		rawMenus := propList(tc.props, tc.lists, "menu")
		menus := make([]domain.MenuDef, len(rawMenus))
		for i, m := range rawMenus {
			menus[i] = domain.MenuDef{Label: m}
		}
		openMenu := propInt(tc.props, "open_menu", -1)
		rawItems := propList(tc.props, tc.lists, "item")
		if openMenu >= 0 && openMenu < len(menus) && len(rawItems) > 0 {
			items := make([]domain.MenuItemDef, len(rawItems))
			for i, label := range rawItems {
				items[i] = domain.MenuItemDef{Label: label}
			}
			menus[openMenu].Items = items
		}
		cursor := propInt(tc.props, "menu_cursor", -1)
		dropCursor := propInt(tc.props, "drop_cursor", 0)
		menuFocused := propBool(tc.props, "menu_focused", false)
		overlay := &OverlayState{
			MenuCursor:  cursor,
			MenuFocused: menuFocused || openMenu >= 0,
			OpenMenu:    openMenu,
			DropCursor:  dropCursor,
		}
		mb := &MenuBar{Menus: menus, Overlay: overlay}
		mb.Render(buf, 0, 0, w, 1, false, layer)
		if openMenu >= 0 {
			dd := overlay.RenderDropdown(menus, 0, layer)
			if dd.Content != "" {
				sub := render.NewImageBuffer(dd.Width, dd.Height)
				sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, layer.Fg, layer.Bg)
				buf.Blit(dd.Col, dd.Row, sub)
			}
		}
		return normaliseWidgetOutput(buf.ToString(profile))
	}

	// All other widgets are wrapped in a Window.
	win, w2, h2 := buildTestWindow(tc)
	buf2 := render.NewImageBuffer(w2, h2)
	win.RenderToBuf(buf2, 0, 0, w2, h2, layer)
	return normaliseWidgetOutput(buf2.ToString(profile))
}

// normaliseWidgetOutput strips ANSI codes and trims trailing spaces per line
// and trailing blank lines, matching the rendertest normalisation convention.
func normaliseWidgetOutput(s string) string {
	stripped := ansi.Strip(s)
	lines := strings.Split(stripped, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	// Drop trailing blank lines.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// ─── File rewriter ───────────────────────────────────────────────────────────

// rewriteEvalFile writes an updated eval file back to disk, replacing the
// expected sections with the actual rendered outputs stored in cases.
func rewriteEvalFile(path string, cases []widgetCase) error {
	var sb strings.Builder
	for i, c := range cases {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("=== " + c.name + "\n")
		if c.configRaw != "" {
			sb.WriteString(c.configRaw + "\n")
		}
		sb.WriteString("===\n")
		if c.expected != "" {
			sb.WriteString(c.expected + "\n")
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// ─── Test entry point ────────────────────────────────────────────────────────

func TestWidgetHarness(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no eval files in testdata/")
	}

	for _, f := range files {
		f := f
		widgetName := strings.TrimSuffix(filepath.Base(f), ".txt")
		t.Run(widgetName, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			cases := parseWidgetEvalFile(string(data))
			if len(cases) == 0 {
				t.Skip("no test cases found")
			}

			updated := false
			for i, tc := range cases {
				tc := tc
				i := i
				t.Run(tc.name, func(t *testing.T) {
					// Run both color profiles; both should produce the same
					// normalised (ANSI-stripped) output.
					for _, profile := range []colorprofile.Profile{
						colorprofile.TrueColor,
						colorprofile.NoTTY,
					} {
						got := renderCase(tc, profile)
						if *updateWidgets {
							cases[i].expected = got
							cases[i].hasExpected = true
							updated = true
							return
						}
						if !tc.hasExpected {
							t.Fatalf("no expected output; run with -update-widgets to generate")
						}
						if got != tc.expected {
							t.Errorf("profile %v mismatch\ngot:\n%s\n\nwant:\n%s",
								profile, got, tc.expected)
						}
					}
				})
			}

			if updated {
				if err := rewriteEvalFile(f, cases); err != nil {
					t.Fatalf("failed to rewrite eval file: %v", err)
				}
			}
		})
	}
}
