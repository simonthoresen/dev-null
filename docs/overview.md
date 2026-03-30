# INSTRUCTIONS.md: Project "null-space" Specification

## PHASE 0: Mandatory Research & API Alignment
The agent MUST use browsing tools to verify the latest versions of the following libraries. Do not use deprecated 'wish' middleware patterns.
- **Wish (SSH):** https://github.com/charmbracelet/wish (Check `bubbletea.Middleware`)
- **Bubble Tea (TUI):** https://github.com/charmbracelet/bubbletea (Check `Model` interface)
- **Lip Gloss (Styles):** https://github.com/charmbracelet/lipgloss (Check Layout/Flexbox tools)
- **Bubbles (Components):** https://github.com/charmbracelet/bubbles (Check `textinput` and `viewport`)

---

## 1. High-Level Architecture
**null-space** is a "Multitenant Singleton" server.
- **The Game Singleton:** A single instance of the Game logic runs on the server.
- **The Program Per User:** Every SSH connection spawns a unique Bubble Tea `Program`, but all programs point to the SAME `Model` instance.
- **The Chrome Wrapper:** The Server wraps the Game's `View()` output inside a global UI (Header, Chat, Input).

## 2. Directory Structure
- `/cmd/null-space`: `main.go` (Flag parsing for `--game` and `--password`).
- `/server`: 
    - `server.go`: Wish server setup and SSH handlers.
    - `chrome.go`: The UI wrapper logic (Header, Chat, Input).
    - `commands.go`: The `/` command registry and permission logic.
- `/common`: 
    - `interfaces.go`: Definition of `Game` and `Player`.
    - `types.go`: Shared structs for `Message` and `Point`.
- `/games`: 
    - `/towerdefense`: The TD implementation.
- `/scripts`: `start.ps1` (Windows automation).

## 3. The Technical Contract (Interfaces)

### A. The Game Interface (`/common/interfaces.go`)
The Game must be "ID-Aware" to support individual viewports and private HUD data.
```go
type Game interface {
    // Init starts background processes (e.g., creep spawning ticker)
    Init() []tea.Cmd
    // Update handles global game logic + player-specific inputs
    Update(msg tea.Msg, playerID string) []tea.Cmd
    // View returns the raw game map string tailored to the player's camera
    View(playerID string, width, height int) string
    // GetCommands returns game-specific commands to register in the framework
    GetCommands() []Command
}
```

### B. The Shared State (`/server/state.go`)
The server must manage a `CentralState` struct to coordinate all sessions:
```go
type CentralState struct {
    sync.RWMutex
    ActiveGame  Game
    Players     map[string]*Player // Key is ssh.SessionID
    ChatHistory []string           // Max 50 messages
    StartTime   time.Time
}
```

## 4. UI Framework & "The Chrome"
The server renders the final screen by stitching pieces together with Lip Gloss. Use `\x1b[H` to reset cursor position and prevent flickering.

1. **Zone 1: Header (1 Row)**
   - Content: `[null-space] | Game: [Name] | [PlayerCount] | Tunnel: [MM:SS]`
   - Style: High-contrast/Reverse video or bold border.

2. **Zone 2: Game Viewport (Flexible)**
   - The server calls `Game.View(playerID, w, h-7)`.
   - The Game must implement a **Scrolling Camera**: Return a slice of the 2D grid centered on the player's global coordinates.

3. **Zone 3: Chat Box (5 Rows)**
   - A `viewport` component showing the `ChatHistory`.
   - Must support "System" messages (e.g., "Player X joined").

4. **Zone 4: Input Line (1 Row)**
   - A `textinput` component. 
   - Toggle focus with `Enter` (Focus/Submit) and `Esc` (Unfocus/Cancel).

## 5. Command & Admin Logic
- **Admin Elevation:** `/admin <password>`. If success, set `Player.IsAdmin = true`.
- **Command Handling:** If input starts with `/`, split by space. Check the `CommandRegistry`.
- **Access Control:** If `Command.AdminOnly` is true and `Player.IsAdmin` is false, return "Permission Denied" to that user's chat.

## 6. Real-Time Synchronization
- **The Global Tick:** The server runs a central `time.Ticker` (approx 100ms).
- **The Broadcast:** On every tick, the server sends a custom `TickMsg` to ALL active Bubble Tea programs.
- **The Result:** Every user's screen re-renders simultaneously, showing moving creeps and other players' cursors in real-time.

## 7. Windows Automation (`/scripts/start.ps1`)
The script must handle background process orchestration:
1. Run `go run ./cmd/null-space --game $args[0] --password $args[1]` as a background task.
2. Run `ssh -p 443 -o ServerAliveInterval=30 -R0:127.0.0.1:23234 tcp@a.pinggy.io`.
3. Use a `while` loop to monitor Pinggy output; parse the `tcp://...` address using Regex.
4. Clear terminal and print a high-visibility "LOBBY OPEN" banner with the exact connection string.

## 8. Development Constraints
- **Bandwidth:** Do not send the full map. Only the visible camera window.
- **Viewport Math:** Explicitly subtract 7 rows from terminal height to ensure the game board never overlaps the UI elements.
- **Session Lifecycle:** On SSH `EOF` or disconnect, delete the player from `CentralState` and broadcast the departure to the chat.

## 9. Client startup, no install, sharable ssh script with punch
1. **The Logic String:**
   ```powershell
   $script = {
       $url = "[PINGGY_URL]"; $p = [PORT];
       $d = ssh -p $p $url 'get-punch-info' | ConvertFrom-StringData;
       if ($d.SERVER_IP) {
           Write-Host "Attempting Direct Punch..." -Fore Yellow;
           ssh -o ConnectTimeout=5 -p $d.SERVER_PORT $d.SERVER_IP;
           if ($LASTEXITCODE -ne 0) { ssh -p $p $url }
       } else { ssh -p $p $url }
   }.ToString()
   ```
2. **The Encoding:** The script must convert this to a UTF-16LE Base64 string.
3. **The Output:** Display a block: 
   `INVITE LINK: powershell -ExecutionPolicy Bypass -EncodedCommand [BASE64]`