package widget

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"null-space/internal/domain"
	"null-space/internal/render"
)

// ─── StatusBar ──────────────────────────────────────────────────────────────

func TestStatusBarRender(t *testing.T) {
	sb := &StatusBar{LeftText: "hello", RightText: "world"}
	out := renderControl(sb, 40, 1, false, testLayer())
	stripped := stripANSI(out)
	if !strings.HasPrefix(stripped, "hello") {
		t.Errorf("expected left text 'hello', got %q", stripped)
	}
	if !strings.HasSuffix(strings.TrimRight(stripped, " "), "world") {
		t.Errorf("expected right text 'world' at end, got %q", stripped)
	}
}

func TestStatusBarEmptyTexts(t *testing.T) {
	sb := &StatusBar{}
	out := renderControl(sb, 20, 1, false, testLayer())
	stripped := stripANSI(out)
	// Should be all spaces.
	if strings.TrimSpace(stripped) != "" {
		t.Errorf("expected only spaces for empty status bar, got %q", stripped)
	}
}

func TestStatusBarZeroSize(t *testing.T) {
	sb := &StatusBar{LeftText: "x"}
	// Should not panic.
	renderControl(sb, 0, 0, false, testLayer())
	renderControl(sb, 0, 1, false, testLayer())
	renderControl(sb, 1, 0, false, testLayer())
}

// ─── MenuBar ────────────────────────────────────────────────────────────────

func TestMenuBarRender(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{
		Menus: []domain.MenuDef{
			{Label: "&File"},
			{Label: "&Help"},
		},
		Overlay: overlay,
	}
	out := renderControl(mb, 40, 1, false, testLayer())
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "File") {
		t.Errorf("expected 'File' in menu bar, got %q", stripped)
	}
	if !strings.Contains(stripped, "Help") {
		t.Errorf("expected 'Help' in menu bar, got %q", stripped)
	}
}

func TestMenuBarRenderFocused(t *testing.T) {
	overlay := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: -1}
	mb := &MenuBar{
		Menus:   []domain.MenuDef{{Label: "&File"}, {Label: "&Edit"}},
		Overlay: overlay,
	}
	out := renderControl(mb, 40, 1, false, testLayer())
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "File") {
		t.Errorf("expected 'File' in focused menu bar, got %q", stripped)
	}
}

func TestMenuBarRenderEmpty(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{Overlay: overlay}
	out := renderControl(mb, 20, 1, false, testLayer())
	if len(stripANSI(out)) == 0 {
		t.Error("expected non-empty output for empty menu bar (should fill background)")
	}
}

func TestMenuBarZeroSize(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{Overlay: overlay}
	renderControl(mb, 0, 0, false, testLayer())
}

// ─── Label ──────────────────────────────────────────────────────────────────

func TestLabelRenderLeft(t *testing.T) {
	l := &Label{Text: "hello"}
	buf := render.NewImageBuffer(20, 1)
	l.Render(buf, 0, 0, 20, 1, false, testLayer())
	if buf.Pixels[0].Char != 'h' {
		t.Errorf("expected 'h' at position 0, got %c", buf.Pixels[0].Char)
	}
}

func TestLabelRenderCenter(t *testing.T) {
	l := &Label{Text: "hi", Align: "center"}
	buf := render.NewImageBuffer(10, 1)
	l.Render(buf, 0, 0, 10, 1, false, testLayer())
	// "hi" is 2 chars, centered in 10 → starts at column 4.
	if buf.Pixels[4].Char != 'h' {
		t.Errorf("expected 'h' at position 4 (centered), got %c", buf.Pixels[4].Char)
	}
}

func TestLabelRenderRight(t *testing.T) {
	l := &Label{Text: "hi", Align: "right"}
	buf := render.NewImageBuffer(10, 1)
	l.Render(buf, 0, 0, 10, 1, false, testLayer())
	// "hi" is 2 chars, right-aligned in 10 → starts at column 8.
	if buf.Pixels[8].Char != 'h' {
		t.Errorf("expected 'h' at position 8 (right), got %c", buf.Pixels[8].Char)
	}
}

// ─── GameView ───────────────────────────────────────────────────────────────

func TestGameViewRenderDefault(t *testing.T) {
	gv := &GameView{}
	// No RenderFn → should fill with spaces.
	buf := render.NewImageBuffer(10, 5)
	gv.Render(buf, 0, 0, 10, 5, false, testLayer())
	if buf.Pixels[0].Char != ' ' {
		t.Errorf("expected space for default render, got %c", buf.Pixels[0].Char)
	}
}

func TestGameViewRenderWithFn(t *testing.T) {
	gv := &GameView{
		RenderFn: func(buf *render.ImageBuffer, x, y, w, h int) {
			buf.SetChar(x, y, 'G', nil, nil, 0)
		},
	}
	buf := render.NewImageBuffer(10, 5)
	gv.Render(buf, 0, 0, 10, 5, false, testLayer())
	if buf.Pixels[0].Char != 'G' {
		t.Errorf("expected 'G' from RenderFn, got %c", buf.Pixels[0].Char)
	}
}

func TestGameViewUpdate(t *testing.T) {
	var captured string
	gv := &GameView{
		focusable: true,
		OnKey: func(key string) {
			captured = key
		},
	}

	// Regular key goes to OnKey.
	gv.Update(tea.KeyPressMsg{Code: 'a'})
	if captured != "a" {
		t.Errorf("expected 'a', got %q", captured)
	}

	// Enter triggers tab.
	gv.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fwd, _ := gv.TabWant()
	if !fwd {
		t.Error("expected WantTab after Enter")
	}

	// Tab triggers tab.
	gv.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	fwd, _ = gv.TabWant()
	if !fwd {
		t.Error("expected WantTab after Tab")
	}
}

func TestGameViewFocusable(t *testing.T) {
	gv := &GameView{}
	if gv.Focusable() {
		t.Error("expected not focusable by default")
	}
	gv.SetFocusable(true)
	if !gv.Focusable() {
		t.Error("expected focusable after SetFocusable(true)")
	}
}

// ─── TeamPanel ──────────────────────────────────────────────────────────────

func TestTeamPanelRender(t *testing.T) {
	tp := &TeamPanel{
		Teams: []domain.Team{
			{Name: "Red", Color: "#ff0000", Players: []string{"p1"}},
		},
		Unassigned: []string{"p2"},
		MyTeamIdx:  0,
		PlayerID:   "p1",
		GetPlayer: func(id string) *domain.Player {
			switch id {
			case "p1":
				return &domain.Player{ID: "p1", Name: "Alice"}
			case "p2":
				return &domain.Player{ID: "p2", Name: "Bob"}
			}
			return nil
		},
	}
	buf := render.NewImageBuffer(30, 10)
	tp.Render(buf, 0, 0, 30, 10, true, testLayer())
	out := buf.ToString(colorprofile.TrueColor)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "Unassigned") {
		t.Errorf("expected 'Unassigned' header, got %q", stripped)
	}
	if !strings.Contains(stripped, "Bob") {
		t.Errorf("expected 'Bob' in unassigned, got %q", stripped)
	}
	if !strings.Contains(stripped, "Red") {
		t.Errorf("expected 'Red' team, got %q", stripped)
	}
	if !strings.Contains(stripped, "Alice") {
		t.Errorf("expected 'Alice' in team, got %q", stripped)
	}
}

func TestTeamPanelRenderEditing(t *testing.T) {
	tp := &TeamPanel{
		Teams: []domain.Team{
			{Name: "Red", Color: "#ff0000", Players: []string{"p1"}},
		},
		MyTeamIdx: 0,
		Editing:   true,
		EditValue: "Blue Team",
		GetPlayer: func(id string) *domain.Player {
			return &domain.Player{ID: id, Name: id}
		},
	}
	buf := render.NewImageBuffer(30, 10)
	tp.Render(buf, 0, 0, 30, 10, true, testLayer())
	stripped := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(stripped, "Blue Team") {
		t.Errorf("expected edit value 'Blue Team', got %q", stripped)
	}
}

func TestTeamPanelRenderWithCreate(t *testing.T) {
	tp := &TeamPanel{
		ShowCreate: true,
		GetPlayer:  func(id string) *domain.Player { return nil },
	}
	buf := render.NewImageBuffer(30, 10)
	tp.Render(buf, 0, 0, 30, 10, false, testLayer())
	stripped := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(stripped, "Create Team") {
		t.Errorf("expected '[+ Create Team]' button, got %q", stripped)
	}
}

func TestTeamPanelUpdate(t *testing.T) {
	moved := -99
	tp := &TeamPanel{
		Teams:       []domain.Team{{Name: "A", Players: []string{"p1"}}},
		MyTeamIdx:   0,
		OnMoveToTeam: func(idx int) { moved = idx },
	}
	// Up from first team → unassigned (-1).
	tp.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if moved != -1 {
		t.Errorf("expected move to -1 (unassigned), got %d", moved)
	}
}

// ─── TruncateStr ────────────────────────────────────────────────────────────

func TestTruncateStr(t *testing.T) {
	if got := TruncateStr("hello world", 5); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := TruncateStr("hi", 5); got != "hi" {
		t.Errorf("expected 'hi', got %q", got)
	}
	if got := TruncateStr("hello", 0); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ─── Screen ─────────────────────────────────────────────────────────────────

func TestScreenRenderToBuf(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{
		Menus:   []domain.MenuDef{{Label: "&File"}},
		Overlay: overlay,
	}
	sb := &StatusBar{LeftText: "status"}
	ti := newTestTextInput()
	input := &CommandInput{TextInput: TextInput{Model: ti}}
	win := &Window{
		FocusIdx: 0,
		Children: []GridChild{
			{Control: input, TabIndex: 0, Constraint: GridConstraint{
				Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal,
			}},
		},
	}
	screen := &Screen{MenuBar: mb, Window: win, StatusBar: sb}

	buf := render.NewImageBuffer(40, 10)
	screen.RenderToBuf(buf, 0, 0, 40, 10, testTheme())

	out := buf.ToString(colorprofile.TrueColor)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "File") {
		t.Errorf("expected 'File' in menu bar, got %q", stripped)
	}
	if !strings.Contains(stripped, "status") {
		t.Errorf("expected 'status' in status bar, got %q", stripped)
	}

	// Check accessor methods.
	if screen.MenuBarRow() != 0 {
		t.Errorf("expected MenuBarRow 0, got %d", screen.MenuBarRow())
	}
	if screen.WindowY() != 1 {
		t.Errorf("expected WindowY 1, got %d", screen.WindowY())
	}
	if screen.WindowH() != 8 {
		t.Errorf("expected WindowH 8, got %d", screen.WindowH())
	}
}

func TestScreenHandleClick(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{Overlay: overlay}
	sb := &StatusBar{}
	win := &Window{Children: []GridChild{}}
	screen := &Screen{MenuBar: mb, Window: win, StatusBar: sb}

	buf := render.NewImageBuffer(40, 10)
	screen.RenderToBuf(buf, 0, 0, 40, 10, testTheme())

	// Click in window area (y=1..8).
	screen.HandleClick(5, 3)

	// Click outside (menu bar row 0).
	consumed := screen.HandleClick(5, 0)
	if consumed {
		t.Error("expected click on menu bar row to not be consumed by Screen")
	}
}

func TestScreenCursorModel(t *testing.T) {
	overlay := &OverlayState{OpenMenu: -1}
	mb := &MenuBar{Overlay: overlay}
	sb := &StatusBar{}
	ti := newTestTextInput()
	input := &CommandInput{TextInput: TextInput{Model: ti}}
	win := &Window{
		FocusIdx: 0,
		Children: []GridChild{
			{Control: input, TabIndex: 0, Constraint: GridConstraint{Col: 0, Row: 0}},
		},
	}
	screen := &Screen{MenuBar: mb, Window: win, StatusBar: sb}

	cm := screen.CursorModel()
	if cm == nil {
		t.Fatal("expected non-nil CursorModel for focused CommandInput")
	}
}

// ─── OverlayState navigation ────────────────────────────────────────────────

func TestOverlayIsActive(t *testing.T) {
	o := &OverlayState{OpenMenu: -1}
	if o.IsActive() {
		t.Error("expected not active initially")
	}
	o.MenuFocused = true
	if !o.IsActive() {
		t.Error("expected active when MenuFocused")
	}
	o.MenuFocused = false
	o.OpenMenu = 0
	if !o.IsActive() {
		t.Error("expected active when OpenMenu >= 0")
	}
	o.OpenMenu = -1
	o.PushDialog(domain.DialogRequest{Title: "Test"})
	if !o.IsActive() {
		t.Error("expected active when dialog open")
	}
}

func TestHandleKeyMenuBarNavigation(t *testing.T) {
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&Open"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Undo"}}},
	}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: -1}

	// Right arrow moves cursor.
	o.HandleKey("right", menus, "p1")
	if o.MenuCursor != 1 {
		t.Errorf("expected cursor 1, got %d", o.MenuCursor)
	}

	// Right wraps around.
	o.HandleKey("right", menus, "p1")
	if o.MenuCursor != 0 {
		t.Errorf("expected cursor 0 (wrap), got %d", o.MenuCursor)
	}

	// Down opens dropdown.
	o.HandleKey("down", menus, "p1")
	if o.OpenMenu != 0 {
		t.Errorf("expected OpenMenu 0, got %d", o.OpenMenu)
	}

	// Esc closes dropdown.
	o.HandleKey("esc", menus, "p1")
	if o.OpenMenu != -1 {
		t.Errorf("expected OpenMenu -1, got %d", o.OpenMenu)
	}
}

func TestHandleKeyDropdownNavigation(t *testing.T) {
	var called bool
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
			{Label: "&Open", Handler: func(string) { called = true }},
			{Label: "---"},
			{Label: "&Close", Handler: func(string) {}},
		}},
	}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}

	// Down skips separator.
	o.HandleKey("down", menus, "p1")
	if o.DropCursor != 2 {
		t.Errorf("expected cursor 2 (skip separator), got %d", o.DropCursor)
	}

	// Up goes back.
	o.HandleKey("up", menus, "p1")
	if o.DropCursor != 0 {
		t.Errorf("expected cursor 0, got %d", o.DropCursor)
	}

	// Enter activates.
	o.HandleKey("enter", menus, "p1")
	if !called {
		t.Error("expected handler to be called on Enter")
	}
	if o.OpenMenu != -1 {
		t.Error("expected menu to close after Enter")
	}
}

func TestHandleKeyDialogNavigation(t *testing.T) {
	var result string
	o := &OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title:   "Confirm",
		Body:    "Are you sure?",
		Buttons: []string{"Yes", "No"},
		OnClose: func(btn string) { result = btn },
	})

	// Tab cycles focus to the next button.
	o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "tab"})

	// Enter confirms the focused button (should be "No" after one Tab).
	o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if result != "No" {
		t.Errorf("expected 'No', got %q", result)
	}
	if o.HasDialog() {
		t.Error("expected dialog to be dismissed")
	}
}

func TestHandleKeyDialogEsc(t *testing.T) {
	var result string
	o := &OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Buttons: []string{"OK"},
		OnClose: func(btn string) { result = btn },
	})

	consumed, _ := o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "esc"})
	if !consumed {
		t.Error("expected esc to be consumed by dialog")
	}
	if result != "" {
		t.Errorf("expected empty result for Esc, got %q", result)
	}
	if o.HasDialog() {
		t.Error("expected dialog to be dismissed after Esc")
	}
}

func TestNextPrevSelectable(t *testing.T) {
	items := []domain.MenuItemDef{
		{Label: "&Open"},
		{Label: "---"},
		{Label: "&Close"},
	}
	if got := NextSelectable(items, 0); got != 2 {
		t.Errorf("expected 2 (skip separator), got %d", got)
	}
	if got := PrevSelectable(items, 2); got != 0 {
		t.Errorf("expected 0 (skip separator), got %d", got)
	}
	// Already at end.
	if got := NextSelectable(items, 2); got != 2 {
		t.Errorf("expected 2 (no next), got %d", got)
	}
}

func TestHandleClickMenuBar(t *testing.T) {
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&Open", Handler: func(string) {}}}},
	}
	o := &OverlayState{OpenMenu: -1}

	// Click on bar row, over "File" (position ~0-5).
	consumed := o.HandleClick(2, 0, 0, 40, 10, menus, "p1")
	if !consumed {
		t.Error("expected click on menu bar to be consumed")
	}
	if o.OpenMenu != 0 {
		t.Errorf("expected OpenMenu 0, got %d", o.OpenMenu)
	}
}

// ─── TextArea ───────────────────────────────────────────────────────────────

func TestTextAreaRender(t *testing.T) {
	ta := &TextArea{Lines: []string{"hello", "world"}}
	buf := render.NewImageBuffer(20, 3)
	ta.Render(buf, 0, 0, 20, 3, true, testLayer())
	out := buf.ToString(colorprofile.TrueColor)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "hello") {
		t.Errorf("expected 'hello' in output, got %q", stripped)
	}
	if !strings.Contains(stripped, "world") {
		t.Errorf("expected 'world' in output, got %q", stripped)
	}
	// Should have brackets.
	if !strings.Contains(stripped, "[") || !strings.Contains(stripped, "]") {
		t.Error("expected brackets in textarea render")
	}
}

func TestTextAreaRenderEmpty(t *testing.T) {
	ta := &TextArea{}
	buf := render.NewImageBuffer(20, 2)
	ta.Render(buf, 0, 0, 20, 2, false, testLayer())
	stripped := stripANSI(buf.ToString(colorprofile.TrueColor))
	// Should show dots for empty lines.
	if !strings.Contains(stripped, "·") {
		t.Errorf("expected dots in empty textarea, got %q", stripped)
	}
}

func TestTextAreaUpdate(t *testing.T) {
	ta := &TextArea{Lines: []string{"hello"}, CursorRow: 0, CursorCol: 5}

	// Type a character.
	ta.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if ta.Lines[0] != "hello!" {
		t.Errorf("expected 'hello!', got %q", ta.Lines[0])
	}

	// Enter splits line.
	ta.CursorCol = 3
	ta.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(ta.Lines) != 2 {
		t.Fatalf("expected 2 lines after Enter, got %d", len(ta.Lines))
	}
	if ta.Lines[0] != "hel" || ta.Lines[1] != "lo!" {
		t.Errorf("unexpected split: %v", ta.Lines)
	}

	// Backspace merges lines.
	ta.CursorRow = 1
	ta.CursorCol = 0
	ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(ta.Lines) != 1 {
		t.Fatalf("expected 1 line after merge, got %d", len(ta.Lines))
	}
	if ta.Lines[0] != "hello!" {
		t.Errorf("expected 'hello!' after merge, got %q", ta.Lines[0])
	}

	// Arrow keys.
	ta.CursorCol = 3
	ta.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if ta.CursorCol != 2 {
		t.Errorf("expected col 2, got %d", ta.CursorCol)
	}
	ta.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if ta.CursorCol != 3 {
		t.Errorf("expected col 3, got %d", ta.CursorCol)
	}

	// Tab sets WantTab.
	ta.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	fwd, _ := ta.TabWant()
	if !fwd {
		t.Error("expected WantTab after Tab")
	}
}

// ─── Container ──────────────────────────────────────────────────────────────

func TestContainerRenderVertical(t *testing.T) {
	c := &Container{
		Horizontal: false,
		Children: []ContainerChild{
			{Control: &Label{Text: "top"}, Weight: 1},
			{Control: &Label{Text: "bot"}, Weight: 1},
		},
	}
	buf := render.NewImageBuffer(10, 4)
	c.Render(buf, 0, 0, 10, 4, false, testLayer())
	if buf.Pixels[0].Char != 't' {
		t.Errorf("expected 't' at top, got %c", buf.Pixels[0].Char)
	}
}

func TestContainerRenderHorizontal(t *testing.T) {
	c := &Container{
		Horizontal: true,
		Children: []ContainerChild{
			{Control: &Label{Text: "L"}, Weight: 1},
			{Control: &Label{Text: "R"}, Weight: 1},
		},
	}
	buf := render.NewImageBuffer(10, 1)
	c.Render(buf, 0, 0, 10, 1, false, testLayer())
	if buf.Pixels[0].Char != 'L' {
		t.Errorf("expected 'L' at left, got %c", buf.Pixels[0].Char)
	}
	if buf.Pixels[5].Char != 'R' {
		t.Errorf("expected 'R' at right half, got %c", buf.Pixels[5].Char)
	}
}

func TestContainerFocusable(t *testing.T) {
	c := &Container{
		Children: []ContainerChild{
			{Control: &Label{Text: "x"}},
		},
	}
	if c.Focusable() {
		t.Error("expected not focusable (Label is not focusable)")
	}
	gv := &GameView{}
	gv.SetFocusable(true)
	c.Children = append(c.Children, ContainerChild{Control: gv})
	if !c.Focusable() {
		t.Error("expected focusable (GameView is focusable)")
	}
}

func TestContainerUpdateCyclesFocus(t *testing.T) {
	gv1 := &GameView{}
	gv1.SetFocusable(true)
	gv2 := &GameView{}
	gv2.SetFocusable(true)

	c := &Container{
		FocusIdx: -1,
		Children: []ContainerChild{
			{Control: &Label{Text: "sep"}},
			{Control: gv1},
			{Control: gv2},
		},
	}
	// First update should focus first focusable child (index 1).
	c.Update(tea.KeyPressMsg{Code: 'x'})
	if c.FocusIdx != 1 {
		t.Errorf("expected FocusIdx 1, got %d", c.FocusIdx)
	}
}

func TestContainerMinSizeHorizontal(t *testing.T) {
	c := &Container{
		Horizontal: true,
		Children: []ContainerChild{
			{Control: &Button{Label: "OK"}, Fixed: 10},
			{Control: &Button{Label: "Cancel"}, Fixed: 12},
		},
	}
	w, h := c.MinSize()
	if w != 22 {
		t.Errorf("expected width 22 (10+12), got %d", w)
	}
	if h != 1 {
		t.Errorf("expected height 1, got %d", h)
	}
}

func TestContainerMinSizeVertical(t *testing.T) {
	c := &Container{
		Horizontal: false,
		Children: []ContainerChild{
			{Control: &Label{Text: "hello world"}},
			{Control: &Label{Text: "hi"}},
		},
	}
	w, h := c.MinSize()
	if w != 11 { // "hello world"
		t.Errorf("expected width 11, got %d", w)
	}
	if h != 2 {
		t.Errorf("expected height 2, got %d", h)
	}
}

// ─── TextView scrollbar buf ────────────────────────────────────────────────

func TestRenderScrollbarBuf(t *testing.T) {
	buf := render.NewImageBuffer(1, 5)
	RenderScrollbarBuf(buf, 0, 0, 20, 5, 0, nil, nil)
	// Should have some thumb/track characters.
	hasThumb := false
	for i := 0; i < 5; i++ {
		if buf.Pixels[i].Char == '█' {
			hasThumb = true
		}
	}
	if !hasThumb {
		t.Error("expected thumb character in scrollbar")
	}
}

func TestRenderScrollbarBufNoScroll(t *testing.T) {
	buf := render.NewImageBuffer(1, 5)
	// total <= visible: no scrollbar needed, fill with spaces.
	RenderScrollbarBuf(buf, 0, 0, 3, 5, 0, nil, nil)
	for i := 0; i < 5; i++ {
		if buf.Pixels[i].Char != ' ' {
			t.Errorf("expected space at %d when no scroll needed, got %c", i, buf.Pixels[i].Char)
		}
	}
}

// ─── handleMenuBarKey letter shortcut ───────────────────────────────────────

func TestHandleMenuBarKeyLetterShortcut(t *testing.T) {
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&Open", Handler: func(string) {}}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Undo", Handler: func(string) {}}}},
	}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: -1}

	// Press 'e' to jump to Edit menu and open it.
	o.handleMenuBarKey("e", menus)
	if o.MenuCursor != 1 {
		t.Errorf("expected cursor 1 (Edit), got %d", o.MenuCursor)
	}
	if o.OpenMenu != 1 {
		t.Errorf("expected OpenMenu 1, got %d", o.OpenMenu)
	}
}

func TestHandleMenuBarKeyEsc(t *testing.T) {
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: -1}
	o.handleMenuBarKey("esc", []domain.MenuDef{{Label: "&File"}})
	if o.MenuFocused {
		t.Error("expected MenuFocused false after Esc")
	}
}

func TestHandleMenuBarKeyLeft(t *testing.T) {
	menus := []domain.MenuDef{{Label: "&File"}, {Label: "&Edit"}}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: -1}
	o.handleMenuBarKey("left", menus)
	if o.MenuCursor != 1 {
		t.Errorf("expected wrap to 1, got %d", o.MenuCursor)
	}
}

// ─── handleDropdownKey left/right ───────────────────────────────────────────

func TestHandleDropdownKeyLeftRight(t *testing.T) {
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&Open", Handler: func(string) {}}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Undo", Handler: func(string) {}}}},
	}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}

	// Right arrow moves to next menu.
	o.handleDropdownKey("right", menus, "p1")
	if o.OpenMenu != 1 {
		t.Errorf("expected OpenMenu 1, got %d", o.OpenMenu)
	}

	// Left arrow moves back.
	o.handleDropdownKey("left", menus, "p1")
	if o.OpenMenu != 0 {
		t.Errorf("expected OpenMenu 0, got %d", o.OpenMenu)
	}

	// Left wraps.
	o.handleDropdownKey("left", menus, "p1")
	if o.OpenMenu != 1 {
		t.Errorf("expected wrap to 1, got %d", o.OpenMenu)
	}
}

func TestHandleDropdownKeyLetterShortcut(t *testing.T) {
	var called bool
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
			{Label: "&Open", Handler: func(string) { called = true }},
		}},
	}
	o := &OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	o.handleDropdownKey("o", menus, "p1")
	if !called {
		t.Error("expected shortcut 'o' to activate Open handler")
	}
}

// ─── Window HandleClick and CycleFocusBack ──────────────────────────────────

func TestWindowCycleFocusBack(t *testing.T) {
	ti1 := newTestTextInput()
	ti2 := newTestTextInput()
	w := &Window{
		FocusIdx: 1,
		Children: []GridChild{
			{Control: &CommandInput{TextInput: TextInput{Model: ti1}}, TabIndex: 0, Constraint: GridConstraint{Col: 0, Row: 0}},
			{Control: &CommandInput{TextInput: TextInput{Model: ti2}}, TabIndex: 1, Constraint: GridConstraint{Col: 0, Row: 1}},
		},
	}
	w.CycleFocusBack()
	if w.FocusIdx != 0 {
		t.Errorf("expected FocusIdx 0 after CycleFocusBack, got %d", w.FocusIdx)
	}
}

// ─── ItemShortcut ───────────────────────────────────────────────────────────

func TestItemShortcut(t *testing.T) {
	item := domain.MenuItemDef{Label: "&Save"}
	if r := ItemShortcut(item); r != 's' {
		t.Errorf("expected 's', got %c", r)
	}
	item2 := domain.MenuItemDef{Label: "No shortcut"}
	if r := ItemShortcut(item2); r != 0 {
		t.Errorf("expected 0, got %c", r)
	}
}

// ─── TextInput Value/SetValue ───────────────────────────────────────────────

func TestTextInputValueSetValue(t *testing.T) {
	m := newTestTextInput()
	ti := &TextInput{Model: m}
	ti.SetValue("hello")
	if ti.Value() != "hello" {
		t.Errorf("expected 'hello', got %q", ti.Value())
	}
}
