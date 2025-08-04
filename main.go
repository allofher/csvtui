package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type model struct {
	csvData      [][]string
	filename     string
	originalData [][]string
	savePrompt   bool
	hasChanges   bool

	// Active CSV data (what's currently being displayed)
	activeHeaders     []string
	activeRows        [][]string
	activeColumnTypes []DataType

	// Original CSV data (before any filtering)
	originalHeaders     []string
	originalRows        [][]string
	originalColumnTypes []DataType

	// Navigation and display
	cursorRow int
	cursorCol int
	viewportX int
	viewportY int
	width     int
	height    int
	renderer  *lipgloss.Renderer

	// Input modes
	editMode       bool
	textInput      textinput.Model
	gotoMode       bool
	gotoStep       int // 0 = row input, 1 = column input
	rowInput       textinput.Model
	colInput       textinput.Model
	gotoError      string
	searchMode     bool
	searchStep     int // 0 = search term, 1 = row filter, 2 = column filter
	searchInput    textinput.Model
	searchRowInput textinput.Model
	searchColInput textinput.Model
	searchResults  [][]int // Array of [row, col] pairs
	searchIndex    int     // Current position in search results
	hasSearched    bool    // Whether a search has been performed

	// Filter functionality
	filterMode         bool // Whether we're in filter input mode
	filterInput        textinput.Model
	isFiltered         bool     // Whether data is currently filtered
	appliedFilters     []string // History of applied filters
	saveFilteredPrompt bool     // Whether to show save filtered CSV prompt
	saveFilteredInput  textinput.Model

	// UI components
	keys       keyMap
	help       help.Model
	config     *Config
	typeColors map[DataType]lipgloss.Color
	dimColors  map[DataType]lipgloss.Color
}

func readCSV(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %v", filename, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error reading CSV file: %v", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	return records, nil
}

func writeCSV(filename string, data [][]string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating file %s: %v", filename, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	for _, record := range data {
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("error writing CSV record: %v", err)
		}
	}

	return nil
}

func (m *model) writeBackup() error {
	backupFilename := m.filename + ".temp"
	return writeCSV(backupFilename, m.csvData)
}

func (m *model) saveToOriginal() error {
	if err := writeCSV(m.filename, m.csvData); err != nil {
		return err
	}

	// Remove backup file after successful save
	backupFilename := m.filename + ".temp"
	os.Remove(backupFilename) // Ignore error if file doesn't exist

	m.hasChanges = false
	return nil
}

type DataType int

const (
	DataTypeString DataType = iota
	DataTypeInt
	DataTypeFloat
	DataTypeBool
	DataTypeEmpty
)

type Config struct {
	Colors  ColorConfig  `json:"colors,omitempty"`
	Hotkeys HotkeyConfig `json:"hotkeys,omitempty"`
}

type ColorConfig struct {
	DataTypeString string `json:"DataTypeString,omitempty"`
	DataTypeInt    string `json:"DataTypeInt,omitempty"`
	DataTypeFloat  string `json:"DataTypeFloat,omitempty"`
	DataTypeBool   string `json:"DataTypeBool,omitempty"`
	DataTypeEmpty  string `json:"DataTypeEmpty,omitempty"`
}

type HotkeyConfig struct {
	Up           []string `json:"Up,omitempty"`
	Down         []string `json:"Down,omitempty"`
	Left         []string `json:"Left,omitempty"`
	Right        []string `json:"Right,omitempty"`
	PageUp       []string `json:"PageUp,omitempty"`
	PageDown     []string `json:"PageDown,omitempty"`
	PageLeft     []string `json:"PageLeft,omitempty"`
	PageRight    []string `json:"PageRight,omitempty"`
	Edit         []string `json:"Edit,omitempty"`
	Help         []string `json:"Help,omitempty"`
	Quit         []string `json:"Quit,omitempty"`
	Save         []string `json:"Save,omitempty"`
	Cancel       []string `json:"Cancel,omitempty"`
	GoTo         []string `json:"GoTo,omitempty"`
	Search       []string `json:"Search,omitempty"`
	NextMatch    []string `json:"NextMatch,omitempty"`
	PrevMatch    []string `json:"PrevMatch,omitempty"`
	Tab          []string `json:"Tab,omitempty"`
	Filter       []string `json:"Filter,omitempty"`
	ResetFilters []string `json:"ResetFilters,omitempty"`
}

func loadConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %v", err)
	}

	configPath := filepath.Join(homeDir, ".csvtui.json")

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Config file doesn't exist, return empty config (will use defaults)
		return &Config{}, nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %v", configPath, err)
	}

	// Parse JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %v", configPath, err)
	}

	return &config, nil
}

func getDefaultColors() map[DataType]lipgloss.Color {
	return map[DataType]lipgloss.Color{
		DataTypeString: lipgloss.Color("#87CEEB"), // Sky blue for strings
		DataTypeInt:    lipgloss.Color("#90EE90"), // Light green for integers
		DataTypeFloat:  lipgloss.Color("#FFB6C1"), // Light pink for floats
		DataTypeBool:   lipgloss.Color("#DDA0DD"), // Plum for booleans
		DataTypeEmpty:  lipgloss.Color("#D3D3D3"), // Light gray for empty
	}
}

func getDefaultDimColors() map[DataType]lipgloss.Color {
	return map[DataType]lipgloss.Color{
		DataTypeString: lipgloss.Color("#4682B4"), // Steel blue (dimmer)
		DataTypeInt:    lipgloss.Color("#6B8E23"), // Olive drab (dimmer)
		DataTypeFloat:  lipgloss.Color("#CD5C5C"), // Indian red (dimmer)
		DataTypeBool:   lipgloss.Color("#9370DB"), // Medium purple (dimmer)
		DataTypeEmpty:  lipgloss.Color("#A9A9A9"), // Dark gray (dimmer)
	}
}

func applyConfigColors(config *Config, defaultColors, defaultDimColors map[DataType]lipgloss.Color) (map[DataType]lipgloss.Color, map[DataType]lipgloss.Color) {
	colors := make(map[DataType]lipgloss.Color)
	dimColors := make(map[DataType]lipgloss.Color)

	// Copy defaults
	for k, v := range defaultColors {
		colors[k] = v
	}
	for k, v := range defaultDimColors {
		dimColors[k] = v
	}

	// Apply config overrides
	if config.Colors.DataTypeString != "" {
		colors[DataTypeString] = lipgloss.Color(config.Colors.DataTypeString)
		// Create a dimmer version by using the same color but with reduced brightness
		dimColors[DataTypeString] = lipgloss.Color(config.Colors.DataTypeString)
	}
	if config.Colors.DataTypeInt != "" {
		colors[DataTypeInt] = lipgloss.Color(config.Colors.DataTypeInt)
		dimColors[DataTypeInt] = lipgloss.Color(config.Colors.DataTypeInt)
	}
	if config.Colors.DataTypeFloat != "" {
		colors[DataTypeFloat] = lipgloss.Color(config.Colors.DataTypeFloat)
		dimColors[DataTypeFloat] = lipgloss.Color(config.Colors.DataTypeFloat)
	}
	if config.Colors.DataTypeBool != "" {
		colors[DataTypeBool] = lipgloss.Color(config.Colors.DataTypeBool)
		dimColors[DataTypeBool] = lipgloss.Color(config.Colors.DataTypeBool)
	}
	if config.Colors.DataTypeEmpty != "" {
		colors[DataTypeEmpty] = lipgloss.Color(config.Colors.DataTypeEmpty)
		dimColors[DataTypeEmpty] = lipgloss.Color(config.Colors.DataTypeEmpty)
	}

	return colors, dimColors
}

func getDefaultHotkeys() map[string][]string {
	return map[string][]string{
		"Up":           {"up", "k"},
		"Down":         {"down", "j"},
		"Left":         {"left", "h"},
		"Right":        {"right", "l"},
		"PageUp":       {"pgup", "i"},
		"PageDown":     {"pgdown", "u"},
		"PageLeft":     {"y"},
		"PageRight":    {"o"},
		"Edit":         {"e"},
		"Help":         {"?"},
		"Quit":         {"q", "ctrl+c"},
		"Save":         {"enter"},
		"Cancel":       {"esc"},
		"GoTo":         {"\\"},
		"Search":       {" "},
		"NextMatch":    {"n"},
		"PrevMatch":    {"b"},
		"Tab":          {"tab"},
		"Filter":       {"~"},
		"ResetFilters": {"="},
	}
}

func applyConfigHotkeys(config *Config, defaults map[string][]string) map[string][]string {
	hotkeys := make(map[string][]string)

	// Copy defaults
	for k, v := range defaults {
		hotkeys[k] = make([]string, len(v))
		copy(hotkeys[k], v)
	}

	// Apply config overrides
	if len(config.Hotkeys.Up) > 0 {
		hotkeys["Up"] = config.Hotkeys.Up
	}
	if len(config.Hotkeys.Down) > 0 {
		hotkeys["Down"] = config.Hotkeys.Down
	}
	if len(config.Hotkeys.Left) > 0 {
		hotkeys["Left"] = config.Hotkeys.Left
	}
	if len(config.Hotkeys.Right) > 0 {
		hotkeys["Right"] = config.Hotkeys.Right
	}
	if len(config.Hotkeys.PageUp) > 0 {
		hotkeys["PageUp"] = config.Hotkeys.PageUp
	}
	if len(config.Hotkeys.PageDown) > 0 {
		hotkeys["PageDown"] = config.Hotkeys.PageDown
	}
	if len(config.Hotkeys.PageLeft) > 0 {
		hotkeys["PageLeft"] = config.Hotkeys.PageLeft
	}
	if len(config.Hotkeys.PageRight) > 0 {
		hotkeys["PageRight"] = config.Hotkeys.PageRight
	}
	if len(config.Hotkeys.Edit) > 0 {
		hotkeys["Edit"] = config.Hotkeys.Edit
	}
	if len(config.Hotkeys.Help) > 0 {
		hotkeys["Help"] = config.Hotkeys.Help
	}
	if len(config.Hotkeys.Quit) > 0 {
		hotkeys["Quit"] = config.Hotkeys.Quit
	}
	if len(config.Hotkeys.Save) > 0 {
		hotkeys["Save"] = config.Hotkeys.Save
	}
	if len(config.Hotkeys.Cancel) > 0 {
		hotkeys["Cancel"] = config.Hotkeys.Cancel
	}
	if len(config.Hotkeys.GoTo) > 0 {
		hotkeys["GoTo"] = config.Hotkeys.GoTo
	}
	if len(config.Hotkeys.Search) > 0 {
		hotkeys["Search"] = config.Hotkeys.Search
	}
	if len(config.Hotkeys.NextMatch) > 0 {
		hotkeys["NextMatch"] = config.Hotkeys.NextMatch
	}
	if len(config.Hotkeys.PrevMatch) > 0 {
		hotkeys["PrevMatch"] = config.Hotkeys.PrevMatch
	}
	if len(config.Hotkeys.Tab) > 0 {
		hotkeys["Tab"] = config.Hotkeys.Tab
	}
	if len(config.Hotkeys.Filter) > 0 {
		hotkeys["Filter"] = config.Hotkeys.Filter
	}
	if len(config.Hotkeys.ResetFilters) > 0 {
		hotkeys["ResetFilters"] = config.Hotkeys.ResetFilters
	}

	return hotkeys
}

func createKeyMapFromConfig(hotkeys map[string][]string) keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys(hotkeys["Up"]...),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys(hotkeys["Down"]...),
			key.WithHelp("↓/j", "move down"),
		),
		Left: key.NewBinding(
			key.WithKeys(hotkeys["Left"]...),
			key.WithHelp("←/h", "move left"),
		),
		Right: key.NewBinding(
			key.WithKeys(hotkeys["Right"]...),
			key.WithHelp("→/l", "move right"),
		),
		PageUp: key.NewBinding(
			key.WithKeys(hotkeys["PageUp"]...),
			key.WithHelp("pgup/i", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys(hotkeys["PageDown"]...),
			key.WithHelp("pgdn/u", "page down"),
		),
		PageLeft: key.NewBinding(
			key.WithKeys(hotkeys["PageLeft"]...),
			key.WithHelp("y", "page left"),
		),
		PageRight: key.NewBinding(
			key.WithKeys(hotkeys["PageRight"]...),
			key.WithHelp("o", "page right"),
		),
		Edit: key.NewBinding(
			key.WithKeys(hotkeys["Edit"]...),
			key.WithHelp("e", "edit cell"),
		),
		Help: key.NewBinding(
			key.WithKeys(hotkeys["Help"]...),
			key.WithHelp("?", "toggle help"),
		),
		Quit: key.NewBinding(
			key.WithKeys(hotkeys["Quit"]...),
			key.WithHelp("q", "quit"),
		),
		Save: key.NewBinding(
			key.WithKeys(hotkeys["Save"]...),
			key.WithHelp("enter", "save edit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys(hotkeys["Cancel"]...),
			key.WithHelp("esc", "cancel"),
		),
		GoTo: key.NewBinding(
			key.WithKeys(hotkeys["GoTo"]...),
			key.WithHelp("\\", "go to position"),
		),
		Search: key.NewBinding(
			key.WithKeys(hotkeys["Search"]...),
			key.WithHelp("space", "search"),
		),
		NextMatch: key.NewBinding(
			key.WithKeys(hotkeys["NextMatch"]...),
			key.WithHelp("n", "next match"),
		),
		PrevMatch: key.NewBinding(
			key.WithKeys(hotkeys["PrevMatch"]...),
			key.WithHelp("b", "prev match"),
		),
		Tab: key.NewBinding(
			key.WithKeys(hotkeys["Tab"]...),
			key.WithHelp("tab", "next field"),
		),
		Filter: key.NewBinding(
			key.WithKeys(hotkeys["Filter"]...),
			key.WithHelp("~", "filter data"),
		),
		ResetFilters: key.NewBinding(
			key.WithKeys(hotkeys["ResetFilters"]...),
			key.WithHelp("=", "reset filters"),
		),
	}
}

// keyMap defines keybindings for the CSV TUI
type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	Left         key.Binding
	Right        key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	PageLeft     key.Binding
	PageRight    key.Binding
	Edit         key.Binding
	Help         key.Binding
	Quit         key.Binding
	Save         key.Binding
	Cancel       key.Binding
	GoTo         key.Binding
	Search       key.Binding
	NextMatch    key.Binding
	PrevMatch    key.Binding
	Tab          key.Binding
	Filter       key.Binding
	ResetFilters key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Edit, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},                 // Navigation
		{k.PageUp, k.PageDown, k.PageLeft, k.PageRight}, // Page navigation
		{k.Edit, k.GoTo, k.Search, k.Save, k.Cancel},    // Edit actions
		{k.NextMatch, k.PrevMatch},                      // Search navigation
		{k.Filter, k.ResetFilters},                      // Filter actions
		{k.Help, k.Quit},                                // General
	}
}

func detectDataType(value string) DataType {
	value = strings.TrimSpace(value)

	if value == "" {
		return DataTypeEmpty
	}

	if strings.ToLower(value) == "true" || strings.ToLower(value) == "false" {
		return DataTypeBool
	}

	if _, err := strconv.Atoi(value); err == nil {
		return DataTypeInt
	}

	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return DataTypeFloat
	}

	return DataTypeString
}

func analyzeColumnTypes(rows [][]string) []DataType {
	if len(rows) == 0 {
		return []DataType{}
	}

	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	columnTypes := make([]DataType, maxCols)
	typeCounts := make([]map[DataType]int, maxCols)

	for i := range typeCounts {
		typeCounts[i] = make(map[DataType]int)
	}

	for _, row := range rows {
		for i, cell := range row {
			if i < maxCols {
				dataType := detectDataType(cell)
				typeCounts[i][dataType]++
			}
		}
	}

	for i := range columnTypes {
		maxCount := 0
		dominantType := DataTypeString

		for dataType, count := range typeCounts[i] {
			if count > maxCount && dataType != DataTypeEmpty {
				maxCount = count
				dominantType = dataType
			}
		}

		columnTypes[i] = dominantType
	}

	return columnTypes
}

func (m model) createColorLegend(styles StyleConfig) string {
	legendItems := []string{}

	typeOrder := []DataType{DataTypeString, DataTypeInt, DataTypeFloat, DataTypeBool, DataTypeEmpty}
	typeNames := map[DataType]string{
		DataTypeString: "str",
		DataTypeInt:    "int",
		DataTypeFloat:  "float",
		DataTypeBool:   "bool",
		DataTypeEmpty:  "empty",
	}

	for _, dataType := range typeOrder {
		typeName := typeNames[dataType]
		color := styles.typeColors[dataType]
		if color != "" {
			coloredText := styles.baseStyle.Foreground(color).Bold(true).Render("■") +
				styles.baseStyle.Foreground(lipgloss.Color("252")).Render(typeName)
			legendItems = append(legendItems, coloredText)
		}
	}

	if len(legendItems) > 0 {
		return "Legend: " + strings.Join(legendItems, " ")
	}
	return ""
}

type StyleConfig struct {
	baseStyle     lipgloss.Style
	headerStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	typeColors    map[DataType]lipgloss.Color
	dimTypeColors map[DataType]lipgloss.Color
	evenRowColor  lipgloss.Color
	oddRowColor   lipgloss.Color
}

func createTableStyles(renderer *lipgloss.Renderer, typeColors, dimTypeColors map[DataType]lipgloss.Color) StyleConfig {
	baseStyle := renderer.NewStyle().Padding(0, 1)
	headerStyle := baseStyle.Foreground(lipgloss.Color("252")).Bold(true)
	selectedStyle := baseStyle.Foreground(lipgloss.Color("#01BE85")).Background(lipgloss.Color("#00432F"))

	return StyleConfig{
		baseStyle:     baseStyle,
		headerStyle:   headerStyle,
		selectedStyle: selectedStyle,
		typeColors:    typeColors,
		dimTypeColors: dimTypeColors,
		evenRowColor:  lipgloss.Color("245"),
		oddRowColor:   lipgloss.Color("252"),
	}
}
func (m *model) adjustViewportAfterResize() {
	// Adjust horizontal viewport if cursor is out of visible area
	startCol, endCol := m.calculateVisibleColumns()
	if m.cursorCol < startCol {
		m.viewportX = m.cursorCol
	} else if m.cursorCol >= endCol {
		// Move viewport right to show cursor
		m.viewportX = m.cursorCol - (endCol - startCol - 1)
		if m.viewportX < 0 {
			m.viewportX = 0
		}
		if m.viewportX >= len(m.activeHeaders) {
			m.viewportX = len(m.activeHeaders) - 1
		}
	}

	// Adjust vertical viewport if cursor is out of visible area
	maxRows := m.height - 7 // Account for table, legend, and status lines
	if maxRows < 1 {
		maxRows = 1
	}

	if m.cursorRow < m.viewportY {
		m.viewportY = m.cursorRow
	} else if m.cursorRow >= m.viewportY+maxRows {
		m.viewportY = m.cursorRow - maxRows + 1
		if m.viewportY < 0 {
			m.viewportY = 0
		}
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width

		// Adjust viewport if necessary after resize
		(&m).adjustViewportAfterResize()
	case tea.KeyMsg:
		// Handle save prompt mode first
		if m.savePrompt {
			switch msg.String() {
			case "y", "Y":
				// Save changes to original file
				if err := m.saveToOriginal(); err != nil {
					// Could show error, but for now just quit anyway
				}
				return m, tea.Quit
			case "n", "N":
				// Don't save, just quit
				return m, tea.Quit
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel save prompt
				m.savePrompt = false
				return m, nil
			}
		}

		// Handle save filtered CSV prompt
		if m.saveFilteredPrompt {
			if key.Matches(msg, m.keys.Save) {
				filename := m.saveFilteredInput.Value()
				if filename != "" {
					// Create filtered CSV data
					filteredData := make([][]string, 0, len(m.activeRows)+1)
					filteredData = append(filteredData, m.activeHeaders)
					filteredData = append(filteredData, m.activeRows...)

					if err := writeCSV(filename, filteredData); err != nil {
						// Could show error, but for now just quit anyway
					}
				}
				return m, tea.Quit
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel save filtered prompt
				m.saveFilteredPrompt = false
				return m, nil
			}

			// Update save filtered input
			var cmd tea.Cmd
			m.saveFilteredInput, cmd = m.saveFilteredInput.Update(msg)
			return m, cmd
		}

		// Handle filter input mode
		if m.filterMode {
			if key.Matches(msg, m.keys.Save) {
				// Apply the filter
				query := m.filterInput.Value()
				if query != "" {
					if err := m.applyFilter(query); err != nil {
						// Could show error in status, but for now just ignore
					}
				}
				m.filterMode = false
				return m, nil
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel filter mode
				m.filterMode = false
				return m, nil
			}

			// Update filter input
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}

		// Handle edit mode
		if m.editMode {
			if key.Matches(msg, m.keys.Save) {
				// Save the edit
				if m.cursorRow < len(m.activeRows) && m.cursorCol < len(m.activeRows[m.cursorRow]) {
					newValue := m.textInput.Value()
					oldValue := m.activeRows[m.cursorRow][m.cursorCol]
					if newValue != oldValue {
						m.activeRows[m.cursorRow][m.cursorCol] = newValue

						// Only mark as changed and update csvData if not filtered
						// When filtered, changes are only to the filtered view
						if !m.isFiltered {
							m.hasChanges = true
							m.csvData[m.cursorRow+1][m.cursorCol] = newValue
						}
					}
				}
				m.editMode = false
				return m, nil
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel edit
				m.editMode = false
				return m, nil
			}

			// Update text input
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
		// Handle goto mode keys
		if m.gotoMode {

			// Clear error when user starts typing
			if m.gotoError != "" {
				m.gotoError = ""
			}

			// Update the appropriate text input
			var cmd tea.Cmd
			if m.gotoStep == 0 {
				m.rowInput, cmd = m.rowInput.Update(msg)
			} else {
				m.colInput, cmd = m.colInput.Update(msg)
			}
			return m, cmd
		}

		// Handle goto mode keys
		if m.gotoMode {
			if key.Matches(msg, m.keys.Save) {
				// Process the current input
				if m.gotoStep == 0 {
					// Validate row input
					rowStr := m.rowInput.Value()
					if rowNum, err := strconv.Atoi(rowStr); err != nil || rowNum < 1 || rowNum > len(m.activeRows) {
						// Invalid row input - show error
						m.gotoError = fmt.Sprintf("Invalid row: valid range 1-%d", len(m.activeRows))
						return m, nil
					}

					// Row input valid, move to column input
					m.gotoError = "" // Clear any previous error
					m.gotoStep = 1
					m.colInput = textinput.New()
					m.colInput.Focus()
					m.colInput.Placeholder = "Enter column number (1-" + strconv.Itoa(len(m.activeHeaders)) + ")"
					return m, textinput.Blink
				} else {
					// Validate column input
					colStr := m.colInput.Value()
					if colNum, err := strconv.Atoi(colStr); err != nil || colNum < 1 || colNum > len(m.activeHeaders) {
						// Invalid column input - show error
						m.gotoError = fmt.Sprintf("Invalid column: valid range 1-%d", len(m.activeHeaders))
						return m, nil
					}

					// Both inputs valid - jump to position
					rowNum, _ := strconv.Atoi(m.rowInput.Value())
					colNum, _ := strconv.Atoi(colStr)

					// Jump to the specified position (convert from 1-based to 0-based)
					m.cursorRow = rowNum - 1
					m.cursorCol = colNum - 1

					// Adjust viewport to show the new cursor position
					m.adjustViewportAfterResize()
					// Exit goto mode
					m.gotoMode = false
					m.gotoStep = 0
					m.gotoError = ""
					return m, nil
				}
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel goto mode
				m.gotoMode = false
				m.gotoStep = 0
				return m, nil
			}

			// Update the appropriate text input
			var cmd tea.Cmd
			if m.gotoStep == 0 {
				m.rowInput, cmd = m.rowInput.Update(msg)
			} else {
				m.colInput, cmd = m.colInput.Update(msg)
			}
			return m, cmd
		}

		// Handle search mode keys
		if m.searchMode {
			if key.Matches(msg, m.keys.Save) {
				// Perform search with filters
				query := m.searchInput.Value()
				rowFilter := m.searchRowInput.Value()
				colFilter := m.searchColInput.Value()
				m.performSearchWithFilters(query, rowFilter, colFilter)
				m.searchMode = false
				m.searchStep = 0
				return m, nil
			}
			if key.Matches(msg, m.keys.Cancel) {
				// Cancel search mode
				m.searchMode = false
				m.searchStep = 0
				return m, nil
			}
			if key.Matches(msg, m.keys.Tab) {
				// Navigate between search inputs
				m.searchStep = (m.searchStep + 1) % 3
				switch m.searchStep {
				case 0:
					m.searchInput.Focus()
					m.searchRowInput.Blur()
					m.searchColInput.Blur()
				case 1:
					m.searchInput.Blur()
					m.searchRowInput.Focus()
					m.searchColInput.Blur()
				case 2:
					m.searchInput.Blur()
					m.searchRowInput.Blur()
					m.searchColInput.Focus()
				}
				return m, textinput.Blink
			}

			// Update the appropriate search input
			var cmd tea.Cmd
			switch m.searchStep {
			case 0:
				m.searchInput, cmd = m.searchInput.Update(msg)
			case 1:
				m.searchRowInput, cmd = m.searchRowInput.Update(msg)
			case 2:
				m.searchColInput, cmd = m.searchColInput.Update(msg)
			}
			return m, cmd
		}
		// Normal navigation mode
		switch {
		case key.Matches(msg, m.keys.Quit):
			// Check if we're viewing filtered data and offer to save
			if m.isFiltered {
				m.saveFilteredPrompt = true
				m.saveFilteredInput = textinput.New()
				m.saveFilteredInput.Focus()
				m.saveFilteredInput.Placeholder = "Enter filename to save filtered CSV (or press Esc to quit without saving)"
				return m, textinput.Blink
			}
			// Check if there are unsaved changes
			if m.hasChanges {
				m.savePrompt = true
				return m, nil
			}
			return m, tea.Quit
		case msg.String() == "ctrl+z":
			return m, tea.Suspend
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Edit):
			// Enter edit mode
			if m.cursorRow < len(m.activeRows) && m.cursorCol < len(m.activeRows[m.cursorRow]) {
				m.editMode = true
				m.textInput = textinput.New()
				m.textInput.Focus()
				m.textInput.SetValue(m.activeRows[m.cursorRow][m.cursorCol])
				m.textInput.CursorEnd()
				return m, textinput.Blink
			}
		case key.Matches(msg, m.keys.GoTo):
			// Enter goto mode
			m.gotoMode = true
			m.gotoStep = 0
			m.gotoError = ""
			m.rowInput = textinput.New()
			m.rowInput.Focus()
			m.rowInput.Placeholder = "Enter row number (1-" + strconv.Itoa(len(m.activeRows)) + ")"
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Search):
			// Enter search mode
			m.searchMode = true
			m.searchStep = 0

			// Initialize all search inputs
			m.searchInput = textinput.New()
			m.searchInput.Focus()
			m.searchInput.Placeholder = "Enter search term..."

			m.searchRowInput = textinput.New()
			m.searchRowInput.Placeholder = "Row filter (1-" + strconv.Itoa(len(m.activeRows)) + ", optional)"

			m.searchColInput = textinput.New()
			m.searchColInput.Placeholder = "Col filter (1-" + strconv.Itoa(len(m.activeHeaders)) + ", optional)"

			return m, textinput.Blink
		case key.Matches(msg, m.keys.Filter):
			// Enter filter mode
			m.filterMode = true
			m.filterInput = textinput.New()
			m.filterInput.Focus()
			m.filterInput.Placeholder = "SELECT col1,col2 WHERE col3 == \"value\""
			return m, textinput.Blink
		case key.Matches(msg, m.keys.ResetFilters):
			// Reset all filters
			m.resetFilters()
		case key.Matches(msg, m.keys.NextMatch):
			// Navigate to next search result
			if m.hasSearched && len(m.searchResults) > 0 {
				m.navigateToSearchResult(m.searchIndex + 1)
			}
		case key.Matches(msg, m.keys.PrevMatch):
			// Navigate to previous search result
			if m.hasSearched && len(m.searchResults) > 0 {
				m.navigateToSearchResult(m.searchIndex - 1)
			}
		case key.Matches(msg, m.keys.Left):
			if m.cursorCol > 0 {
				m.cursorCol--
				// Adjust viewport if cursor moved out of visible area
				if m.cursorCol < m.viewportX {
					m.viewportX = m.cursorCol
				}
			}
		case key.Matches(msg, m.keys.Right):
			if m.cursorCol < len(m.activeHeaders)-1 {
				m.cursorCol++
				// Check if cursor is now out of visible area and adjust viewport
				_, endCol := m.calculateVisibleColumns()
				if m.cursorCol >= endCol {
					// Move viewport right by one column to show the cursor
					m.viewportX++
					// Ensure we don't go beyond the available columns
					if m.viewportX >= len(m.activeHeaders) {
						m.viewportX = len(m.activeHeaders) - 1
					}
				}
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursorRow < len(m.activeRows)-1 {
				m.cursorRow++
				maxRows := m.height - 7 // Account for extra legend line
				if maxRows < 1 {
					maxRows = 1
				}
				if m.cursorRow >= m.viewportY+maxRows {
					m.viewportY++
				}
			}
		case key.Matches(msg, m.keys.Up):
			if m.cursorRow > 0 {
				m.cursorRow--
				if m.cursorRow < m.viewportY {
					m.viewportY = m.cursorRow
				}
			}
		case key.Matches(msg, m.keys.PageDown):
			// Page down - jump by visible rows
			maxRows := m.height - 7 // Account for table, legend, and status lines
			if maxRows < 1 {
				maxRows = 1
			}
			newRow := m.cursorRow + maxRows
			if newRow >= len(m.activeRows) {
				newRow = len(m.activeRows) - 1
			}
			m.cursorRow = newRow
			// Adjust viewport to show the new cursor position
			if m.cursorRow >= m.viewportY+maxRows {
				m.viewportY = m.cursorRow - maxRows + 1
				if m.viewportY < 0 {
					m.viewportY = 0
				}
			}
		case key.Matches(msg, m.keys.PageUp):
			// Page up - jump by visible rows
			maxRows := m.height - 7 // Account for table, legend, and status lines
			if maxRows < 1 {
				maxRows = 1
			}
			newRow := m.cursorRow - maxRows
			if newRow < 0 {
				newRow = 0
			}
			m.cursorRow = newRow
			// Adjust viewport to show the new cursor position
			if m.cursorRow < m.viewportY {
				m.viewportY = m.cursorRow
			}
		case key.Matches(msg, m.keys.PageRight):
			// Page right - jump by visible columns
			startCol, endCol := m.calculateVisibleColumns()
			visibleCols := endCol - startCol
			if visibleCols < 1 {
				visibleCols = 1
			}
			newCol := m.cursorCol + visibleCols
			if newCol >= len(m.activeHeaders) {
				newCol = len(m.activeHeaders) - 1
			}
			m.cursorCol = newCol
			// Adjust viewport to show the new cursor position
			_, currentEndCol := m.calculateVisibleColumns()
			if m.cursorCol >= currentEndCol {
				m.viewportX = m.cursorCol - visibleCols + 1
				if m.viewportX < 0 {
					m.viewportX = 0
				}
				if m.viewportX >= len(m.activeHeaders) {
					m.viewportX = len(m.activeHeaders) - 1
				}
			}
		case key.Matches(msg, m.keys.PageLeft):
			// Page left - jump by visible columns
			startCol, endCol := m.calculateVisibleColumns()
			visibleCols := endCol - startCol
			if visibleCols < 1 {
				visibleCols = 1
			}
			newCol := m.cursorCol - visibleCols
			if newCol < 0 {
				newCol = 0
			}
			m.cursorCol = newCol
			// Adjust viewport to show the new cursor position
			if m.cursorCol < m.viewportX {
				m.viewportX = m.cursorCol
			}
		}
	}
	return m, nil
}
func (m model) calculateColumnWidths() []int {
	if len(m.activeHeaders) == 0 {
		return []int{}
	}

	columnWidths := make([]int, len(m.activeHeaders))

	for i, header := range m.activeHeaders {
		columnWidths[i] = len(header)
	}

	for _, row := range m.activeRows {
		for i, cell := range row {
			if i < len(columnWidths) && len(cell) > columnWidths[i] {
				columnWidths[i] = len(cell)
			}
		}
	}

	for i := range columnWidths {
		if columnWidths[i] < 8 {
			columnWidths[i] = 8
		}
		if columnWidths[i] > 20 {
			columnWidths[i] = 20
		}
	}

	return columnWidths
}

func (m model) calculateVisibleColumns() (int, int) {
	columnWidths := m.calculateColumnWidths()
	if len(columnWidths) == 0 {
		return 0, 0
	}

	// Account for lipgloss table styling overhead:
	// - Left and right table borders: 2 chars
	// - Each column has padding (2 chars total per column)
	// - Column separators between columns: 1 char each
	// - Additional margin for safety: 4 chars
	tableBorderWidth := 2
	marginWidth := 4
	availableWidth := m.width - tableBorderWidth - marginWidth

	startCol := m.viewportX
	if startCol >= len(columnWidths) {
		startCol = len(columnWidths) - 1
	}
	if startCol < 0 {
		startCol = 0
	}

	// Calculate how many columns we can fit starting from startCol
	currentWidth := 0
	endCol := startCol

	for i := startCol; i < len(columnWidths); i++ {
		// Calculate space needed for this column:
		// - column content width
		// - padding (2 chars: 1 on each side)
		// - column separator (1 char, but not for the last column we're considering)
		columnSpace := columnWidths[i] + 2 // content + padding

		// Add separator space if this isn't the first column we're adding
		if i > startCol {
			columnSpace += 1 // separator
		}

		// Check if adding this column would exceed available width
		if currentWidth+columnSpace <= availableWidth {
			currentWidth += columnSpace
			endCol = i + 1
		} else {
			break
		}
	}

	// Ensure we show at least one column
	if endCol <= startCol {
		endCol = startCol + 1
	}

	// Ensure endCol doesn't exceed the number of columns
	if endCol > len(columnWidths) {
		endCol = len(columnWidths)
	}

	return startCol, endCol
}
func (m model) View() string {
	if len(m.activeRows) == 0 {
		return "No data to display"
	}

	styles := createTableStyles(m.renderer, m.typeColors, m.dimColors)

	maxRows := m.height - 7 // Account for table, column info, legend, and status lines
	if maxRows < 1 {
		maxRows = 1
	}

	startRow := m.viewportY
	endRow := startRow + maxRows
	if endRow > len(m.activeRows) {
		endRow = len(m.activeRows)
	}

	startCol, endCol := m.calculateVisibleColumns()

	if endCol > len(m.activeHeaders) {
		endCol = len(m.activeHeaders)
	}

	visibleHeaders := m.activeHeaders[startCol:endCol]
	visibleRows := make([][]string, 0, endRow-startRow)

	for i := startRow; i < endRow; i++ {
		if i < len(m.activeRows) {
			row := make([]string, len(visibleHeaders))
			for j := 0; j < len(visibleHeaders) && startCol+j < len(m.activeRows[i]); j++ {
				row[j] = m.activeRows[i][startCol+j]
			}
			visibleRows = append(visibleRows, row)
		}
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(m.renderer.NewStyle().Foreground(lipgloss.Color("238"))).
		Headers(visibleHeaders...).
		Rows(visibleRows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return styles.headerStyle
			}

			actualRow := startRow + row
			actualCol := startCol + col

			if actualRow == m.cursorRow && actualCol == m.cursorCol {
				return styles.selectedStyle
			}

			even := row%2 == 0

			if actualCol < len(m.activeColumnTypes) {
				columnType := m.activeColumnTypes[actualCol]

				var color lipgloss.Color
				if even {
					color = styles.dimTypeColors[columnType]
				} else {
					color = styles.typeColors[columnType]
				}

				// If we have a color for this data type, use it
				if color != "" {
					return styles.baseStyle.Foreground(color)
				}
				// Otherwise fall through to default alternating row colors
			}

			if even {
				return styles.baseStyle.Foreground(styles.evenRowColor)
			}
			return styles.baseStyle.Foreground(styles.oddRowColor)
		})

	typeInfo := make([]string, 0, len(visibleHeaders))
	for i, header := range visibleHeaders {
		actualCol := startCol + i
		if actualCol < len(m.activeColumnTypes) {
			var typeStr string
			switch m.activeColumnTypes[actualCol] {
			case DataTypeString:
				typeStr = "str"
			case DataTypeInt:
				typeStr = "int"
			case DataTypeFloat:
				typeStr = "float"
			case DataTypeBool:
				typeStr = "bool"
			case DataTypeEmpty:
				typeStr = "empty"
			default:
				typeStr = "unknown"
			}
			typeInfo = append(typeInfo, fmt.Sprintf("%s(%s)", header, typeStr))
		} else {
			typeInfo = append(typeInfo, header)
		}
	}

	// Calculate total width being used
	columnWidths := m.calculateColumnWidths()
	totalUsedWidth := 2 // left and right borders
	for i := startCol; i < endCol; i++ {
		if i < len(columnWidths) {
			totalUsedWidth += columnWidths[i] + 2 // content + padding
			if i > startCol {
				totalUsedWidth += 1 // separator (not for first column)
			}
		}
	}

	legend := m.createColorLegend(styles)

	// Create status info (row/col info, viewport info, modified status, filter status)
	changeIndicator := ""
	if m.hasChanges {
		changeIndicator = " [MODIFIED]"
	}
	filterIndicator := ""
	if m.isFiltered {
		filterIndicator = fmt.Sprintf(" [FILTERED: %d filters]", len(m.appliedFilters))
	}
	statusInfo := fmt.Sprintf("Row: %d/%d, Col: %d/%d | Showing cols %d-%d | Width: %d/%d%s%s",
		m.cursorRow+1, len(m.activeRows), m.cursorCol+1, len(m.activeHeaders), startCol+1, endCol, totalUsedWidth, m.width, changeIndicator, filterIndicator)

	// Handle different modes
	if m.savePrompt {
		savePrompt := fmt.Sprintf("Save changes to %s?", m.filename)
		saveStatus := "You have unsaved changes. Save to original file? (y/n, Esc to cancel)"
		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, savePrompt, saveStatus)
	}

	if m.saveFilteredPrompt {
		savePrompt := "Save filtered CSV as: " + m.saveFilteredInput.View()
		saveStatus := "Enter filename to save filtered data, or Esc to quit without saving"
		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, savePrompt, saveStatus)
	}

	if m.filterMode {
		filterPrompt := "Filter: " + m.filterInput.View()
		filterStatus := "FILTER MODE - Enter SQL-like query (SELECT col1,col2 WHERE col3 == \"value\"), Enter to apply, Esc to cancel"
		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, filterPrompt, filterStatus)
	}

	if m.editMode {
		editPrompt := fmt.Sprintf("Editing cell [%d,%d]: %s", m.cursorRow+1, m.cursorCol+1, m.textInput.View())
		editStatus := "EDIT MODE - Enter to save, Esc to cancel"
		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, editPrompt, editStatus)
	}

	if m.gotoMode {
		var gotoPrompt, gotoStatus string
		if m.gotoStep == 0 {
			gotoPrompt = fmt.Sprintf("Go to row: %s", m.rowInput.View())
			gotoStatus = "GOTO MODE - Enter row number, then press Enter"
		} else {
			gotoPrompt = fmt.Sprintf("Go to row %s, column: %s", m.rowInput.Value(), m.colInput.View())
			gotoStatus = "GOTO MODE - Enter column number, then press Enter (Esc to cancel)"
		}

		// Show error message if there is one
		if m.gotoError != "" {
			errorStyle := m.renderer.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Bold(true)
			gotoStatus = errorStyle.Render(m.gotoError)
		}

		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, gotoPrompt, gotoStatus)
	}

	if m.searchMode {
		// Create focused indicator for current input
		focusIndicator := func(step int) string {
			if m.searchStep == step {
				return "► "
			}
			return "  "
		}

		searchPrompt := fmt.Sprintf("%sSearch: %s", focusIndicator(0), m.searchInput.View())
		rowPrompt := fmt.Sprintf("%sRow filter: %s", focusIndicator(1), m.searchRowInput.View())
		colPrompt := fmt.Sprintf("%sCol filter: %s", focusIndicator(2), m.searchColInput.View())
		searchStatus := "SEARCH MODE - Tab to switch fields, Enter to search, Esc to cancel"

		return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n%s", t.String(), legend, statusInfo, searchPrompt, rowPrompt, colPrompt, searchStatus)
	}

	// Normal mode - show help with search results info
	var statusWithSearch string
	if m.hasSearched {
		if len(m.searchResults) > 0 {
			statusWithSearch = fmt.Sprintf("%s | Search: %d/%d matches (n/b to navigate)",
				statusInfo, m.searchIndex+1, len(m.searchResults))
		} else {
			statusWithSearch = fmt.Sprintf("%s | Search: no matches found", statusInfo)
		}
	} else {
		statusWithSearch = statusInfo
	}

	// Normal mode - show help
	helpView := m.help.View(m.keys)
	return fmt.Sprintf("%s\n%s\n%s\n%s", t.String(), legend, statusWithSearch, helpView)
}

func (m *model) performSearchWithFilters(query, rowFilter, colFilter string) {
	m.searchResults = [][]int{}
	if query == "" {
		return
	}

	queryLower := strings.ToLower(query)

	// Parse row filter (1-based, convert to 0-based)
	var targetRow int = -1
	if rowFilter != "" {
		if rowNum, err := strconv.Atoi(rowFilter); err == nil && rowNum >= 1 && rowNum <= len(m.activeRows) {
			targetRow = rowNum - 1
		}
	}

	// Parse column filter (1-based, convert to 0-based)
	var targetCol int = -1
	if colFilter != "" {
		if colNum, err := strconv.Atoi(colFilter); err == nil && colNum >= 1 && colNum <= len(m.activeHeaders) {
			targetCol = colNum - 1
		}
	}

	// Search through cells with filters applied
	for rowIdx, row := range m.activeRows {
		// Skip row if row filter is specified and doesn't match
		if targetRow != -1 && rowIdx != targetRow {
			continue
		}

		for colIdx, cell := range row {
			// Skip column if column filter is specified and doesn't match
			if targetCol != -1 && colIdx != targetCol {
				continue
			}

			if strings.Contains(strings.ToLower(cell), queryLower) {
				m.searchResults = append(m.searchResults, []int{rowIdx, colIdx})
			}
		}
	}

	// Reset search index
	m.searchIndex = 0
	m.hasSearched = true

	// If we have results, jump to the first one
	if len(m.searchResults) > 0 {
		m.cursorRow = m.searchResults[0][0]
		m.cursorCol = m.searchResults[0][1]
		m.adjustViewportAfterResize()
	}
}
func (m *model) navigateToSearchResult(index int) {
	if len(m.searchResults) == 0 {
		return
	}

	// Handle wrapping
	if index >= len(m.searchResults) {
		index = 0
	} else if index < 0 {
		index = len(m.searchResults) - 1
	}

	m.searchIndex = index
	m.cursorRow = m.searchResults[index][0]
	m.cursorCol = m.searchResults[index][1]
	m.adjustViewportAfterResize()
}

type FilterCondition struct {
	Column   string
	Operator string
	Value    string
}

type FilterQuery struct {
	SelectColumns []string
	Conditions    []FilterCondition
}

func parseFilterQuery(query string, headers []string) (*FilterQuery, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}

	// Create case-insensitive regex patterns
	selectPattern := regexp.MustCompile(`(?i)^select\s+(.+?)(?:\s+where\s+(.+))?$`)
	matches := selectPattern.FindStringSubmatch(query)

	if len(matches) == 0 {
		return nil, fmt.Errorf("invalid query format. Use: SELECT col1,col2 WHERE col3 == \"value\"")
	}

	fq := &FilterQuery{}

	// Parse SELECT columns
	selectPart := strings.TrimSpace(matches[1])
	if selectPart == "*" {
		fq.SelectColumns = headers
	} else {
		columns := strings.Split(selectPart, ",")
		for _, col := range columns {
			col = strings.TrimSpace(col)
			if col != "" {
				// Check if column exists
				found := false
				for _, header := range headers {
					if strings.EqualFold(header, col) {
						fq.SelectColumns = append(fq.SelectColumns, header)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("column '%s' not found", col)
				}
			}
		}
	}

	// Parse WHERE conditions if present
	if len(matches) > 2 && matches[2] != "" {
		wherePart := strings.TrimSpace(matches[2])
		conditions, err := parseWhereConditions(wherePart, headers)
		if err != nil {
			return nil, err
		}
		fq.Conditions = conditions
	}

	return fq, nil
}

func parseWhereConditions(wherePart string, headers []string) ([]FilterCondition, error) {
	var conditions []FilterCondition

	// Split by AND (case-insensitive)
	andPattern := regexp.MustCompile(`(?i)\s+and\s+`)
	conditionParts := andPattern.Split(wherePart, -1)

	for _, part := range conditionParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse individual condition: column operator value
		condPattern := regexp.MustCompile(`(\w+)\s*(==|!=|>=|<=|>|<|LIKE|like)\s*"([^"]*)"`)
		matches := condPattern.FindStringSubmatch(part)

		if len(matches) != 4 {
			return nil, fmt.Errorf("invalid condition format: %s. Use: column == \"value\"", part)
		}

		column := strings.TrimSpace(matches[1])
		operator := strings.TrimSpace(matches[2])
		value := matches[3]

		// Check if column exists
		found := false
		for _, header := range headers {
			if strings.EqualFold(header, column) {
				column = header // Use the actual header name
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("column '%s' not found in WHERE clause", column)
		}

		conditions = append(conditions, FilterCondition{
			Column:   column,
			Operator: strings.ToUpper(operator),
			Value:    value,
		})
	}

	return conditions, nil
}

func (m *model) applyFilter(query string) error {
	// Store original data if this is the first filter
	if !m.isFiltered {
		m.originalHeaders = make([]string, len(m.activeHeaders))
		copy(m.originalHeaders, m.activeHeaders)

		m.originalRows = make([][]string, len(m.activeRows))
		for i, row := range m.activeRows {
			m.originalRows[i] = make([]string, len(row))
			copy(m.originalRows[i], row)
		}

		m.originalColumnTypes = make([]DataType, len(m.activeColumnTypes))
		copy(m.originalColumnTypes, m.activeColumnTypes)
	}

	// Parse the filter query using current active headers
	filterQuery, err := parseFilterQuery(query, m.activeHeaders)
	if err != nil {
		return err
	}

	// Apply column selection based on current active headers
	selectedColumnIndices := make([]int, 0, len(filterQuery.SelectColumns))
	for _, selectedCol := range filterQuery.SelectColumns {
		for i, header := range m.activeHeaders {
			if header == selectedCol {
				selectedColumnIndices = append(selectedColumnIndices, i)
				break
			}
		}
	}

	// Filter current active rows based on WHERE conditions
	var filteredRows [][]string
	for _, row := range m.activeRows {
		if m.rowMatchesCurrentConditions(row, filterQuery.Conditions, m.activeHeaders) {
			// Select only the specified columns
			newRow := make([]string, len(selectedColumnIndices))
			for i, colIdx := range selectedColumnIndices {
				if colIdx < len(row) {
					newRow[i] = row[colIdx]
				}
			}
			filteredRows = append(filteredRows, newRow)
		}
	}

	// Update active data with filtered results
	m.activeHeaders = filterQuery.SelectColumns
	m.activeRows = filteredRows
	m.activeColumnTypes = analyzeColumnTypes(filteredRows)
	m.isFiltered = true
	m.appliedFilters = append(m.appliedFilters, query)

	// Reset cursor position
	m.cursorRow = 0
	m.cursorCol = 0
	m.viewportX = 0
	m.viewportY = 0

	return nil
}

func (m *model) rowMatchesConditions(row []string, conditions []FilterCondition) bool {
	for _, condition := range conditions {
		// Find column index in original headers
		colIndex := -1
		for i, header := range m.originalHeaders {
			if header == condition.Column {
				colIndex = i
				break
			}
		}

		if colIndex == -1 || colIndex >= len(row) {
			return false
		}

		cellValue := row[colIndex]
		if !m.evaluateCondition(cellValue, condition.Operator, condition.Value) {
			return false
		}
	}
	return true
}

func (m *model) rowMatchesCurrentConditions(row []string, conditions []FilterCondition, currentHeaders []string) bool {
	for _, condition := range conditions {
		// Find column index in current headers
		colIndex := -1
		for i, header := range currentHeaders {
			if header == condition.Column {
				colIndex = i
				break
			}
		}

		if colIndex == -1 || colIndex >= len(row) {
			return false
		}

		cellValue := row[colIndex]
		if !m.evaluateCondition(cellValue, condition.Operator, condition.Value) {
			return false
		}
	}
	return true
}
func (m *model) evaluateCondition(cellValue, operator, filterValue string) bool {
	switch operator {
	case "==":
		return strings.EqualFold(cellValue, filterValue)
	case "!=":
		return !strings.EqualFold(cellValue, filterValue)
	case "LIKE":
		return strings.Contains(strings.ToLower(cellValue), strings.ToLower(filterValue))
	case ">":
		if cellFloat, err1 := strconv.ParseFloat(cellValue, 64); err1 == nil {
			if filterFloat, err2 := strconv.ParseFloat(filterValue, 64); err2 == nil {
				return cellFloat > filterFloat
			}
		}
		return cellValue > filterValue
	case "<":
		if cellFloat, err1 := strconv.ParseFloat(cellValue, 64); err1 == nil {
			if filterFloat, err2 := strconv.ParseFloat(filterValue, 64); err2 == nil {
				return cellFloat < filterFloat
			}
		}
		return cellValue < filterValue
	case ">=":
		if cellFloat, err1 := strconv.ParseFloat(cellValue, 64); err1 == nil {
			if filterFloat, err2 := strconv.ParseFloat(filterValue, 64); err2 == nil {
				return cellFloat >= filterFloat
			}
		}
		return cellValue >= filterValue
	case "<=":
		if cellFloat, err1 := strconv.ParseFloat(cellValue, 64); err1 == nil {
			if filterFloat, err2 := strconv.ParseFloat(filterValue, 64); err2 == nil {
				return cellFloat <= filterFloat
			}
		}
		return cellValue <= filterValue
	}
	return false
}

func (m *model) resetFilters() {
	if !m.isFiltered {
		return
	}

	// Restore original data to active data
	m.activeHeaders = make([]string, len(m.originalHeaders))
	copy(m.activeHeaders, m.originalHeaders)

	m.activeRows = make([][]string, len(m.originalRows))
	for i, row := range m.originalRows {
		m.activeRows[i] = make([]string, len(row))
		copy(m.activeRows[i], row)
	}

	m.activeColumnTypes = make([]DataType, len(m.originalColumnTypes))
	copy(m.activeColumnTypes, m.originalColumnTypes)

	// Reset filter state
	m.isFiltered = false
	m.appliedFilters = []string{}

	// Reset cursor position
	m.cursorRow = 0
	m.cursorCol = 0
	m.viewportX = 0
	m.viewportY = 0
}
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <csv-file>\n", os.Args[0])
		os.Exit(1)
	}

	// Load config
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load config: %v\n", err)
		config = &Config{} // Use empty config (defaults will be used)
	}

	// Apply config to colors and hotkeys
	defaultColors := getDefaultColors()
	defaultDimColors := getDefaultDimColors()
	typeColors, dimColors := applyConfigColors(config, defaultColors, defaultDimColors)

	defaultHotkeys := getDefaultHotkeys()
	hotkeys := applyConfigHotkeys(config, defaultHotkeys)
	keyMap := createKeyMapFromConfig(hotkeys)

	filename := os.Args[1]
	records, err := readCSV(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	headers := records[0]
	rows := records[1:]
	columnTypes := analyzeColumnTypes(rows)

	// Create a deep copy of the original data for comparison
	originalData := make([][]string, len(records))
	for i, row := range records {
		originalData[i] = make([]string, len(row))
		copy(originalData[i], row)
	}

	m := model{
		csvData:      records,
		filename:     filename,
		originalData: originalData,
		savePrompt:   false,
		hasChanges:   false,

		// Initialize active data with original data
		activeHeaders:     make([]string, len(headers)),
		activeRows:        make([][]string, len(rows)),
		activeColumnTypes: make([]DataType, len(columnTypes)),

		cursorRow: 0,
		cursorCol: 0,
		viewportX: 0,
		viewportY: 0,
		width:     80,
		height:    24,
		renderer:  lipgloss.NewRenderer(os.Stdout),

		keys:               keyMap,
		help:               help.New(),
		config:             config,
		typeColors:         typeColors,
		dimColors:          dimColors,
		isFiltered:         false,
		appliedFilters:     []string{},
		filterMode:         false,
		saveFilteredPrompt: false,
	}

	// Copy original data to active data
	copy(m.activeHeaders, headers)
	for i, row := range rows {
		m.activeRows[i] = make([]string, len(row))
		copy(m.activeRows[i], row)
	}
	copy(m.activeColumnTypes, columnTypes)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
