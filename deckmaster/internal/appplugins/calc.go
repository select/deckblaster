package appplugins

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

var (
	calcStateFile = "/tmp/streamdeck-calc.json"
	calcTmpDir    = "/tmp/streamdeck-calc"
)

const calcKeyDisplay = 0

type calcState struct {
	Display  string   `json:"display"`
	Operand1 *float64 `json:"operand1"`
	Operator *string  `json:"operator"`
	NewInput bool     `json:"new_input"`
	Error    bool     `json:"error"`
}

func defaultCalcState() calcState {
	return calcState{
		Display:  "0",
		Operand1: nil,
		Operator: nil,
		NewInput: true,
		Error:    false,
	}
}

func loadCalcState() calcState {
	data, err := os.ReadFile(calcStateFile)
	if err != nil {
		return defaultCalcState()
	}
	var s calcState
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultCalcState()
	}
	if s.Display == "" {
		s.Display = "0"
	}
	return s
}

func saveCalcState(s calcState) {
	if err := writeJSONAtomic(calcStateFile, s); err != nil {
		fmt.Fprintf(os.Stderr, "calc save state error: %v\n", err)
	}
}

func formatCalcNumber(val float64) string {
	if math.IsInf(val, 1) {
		return "INF"
	}
	if math.IsInf(val, -1) {
		return "-INF"
	}
	if math.IsNaN(val) {
		return "ERR"
	}
	if val == math.Trunc(val) && math.Abs(val) < 1e12 {
		return strconv.FormatInt(int64(val), 10)
	}
	return strconv.FormatFloat(val, 'g', 8, 64)
}

func doCalculate(a float64, op string, b float64) (float64, bool) {
	switch op {
	case "+":
		return a + b, true
	case "-":
		return a - b, true
	case "*":
		return a * b, true
	case "/":
		if b == 0 {
			return 0, false
		}
		return a / b, true
	default:
		return b, true
	}
}

func renderCalcDisplay(s calcState) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	drawGradientV(img, 0, 0, keySize, keySize,
		color.RGBA{R: 10, G: 22, B: 10, A: 255},
		color.RGBA{R: 18, G: 34, B: 18, A: 255},
	)
	outline := color.RGBA{R: 40, G: 80, B: 40, A: 255}
	drawRoundedRect(img, 2, 2, 70, 70, 6, nil, &outline, 2)

	text := s.Display
	if text == "" {
		text = "0"
	}
	col := color.RGBA{R: 0, G: 255, B: 80, A: 255}
	if s.Error {
		col = color.RGBA{R: 255, G: 60, B: 60, A: 255}
	}

	if s.Operator != nil && !s.NewInput {
		opSym := map[string]string{
			"+": "+",
			"-": "−",
			"*": "×",
			"/": "÷",
		}[*s.Operator]
		if opSym == "" {
			opSym = *s.Operator
		}
		faceSM := getFontFace(fontDejaVuSansBold, 11)
		drawTextTopLeft(img, 6, 4, opSym, color.RGBA{R: 100, G: 200, B: 100, A: 255}, faceSM)
	}

	var fontSize float64
	switch {
	case len(text) <= 4:
		fontSize = 30
	case len(text) <= 6:
		fontSize = 22
	case len(text) <= 9:
		fontSize = 16
	default:
		fontSize = 11
		if len(text) > 14 {
			text = text[:14]
		}
	}

	face := getFontFace(fontDejaVuSansBold, fontSize)
	tw, th := measureText(face, text)
	x := keySize - tw - 8
	y := (keySize-th)/2 + 4
	drawTextTopLeft(img, x, y, text, col, face)

	path := filepath.Join(calcTmpDir, "display.png")
	if err := savePNGAtomic(path, img); err != nil {
		fmt.Fprintf(os.Stderr, "calc render error: %v\n", err)
	}
	return path
}

func updateCalcDisplay(s calcState) {
	path := renderCalcDisplay(s)
	pushIcon(calcKeyDisplay, path)
}

func calcInit() {
	s := loadCalcState()
	updateCalcDisplay(s)
}

func calcDigit(d int) {
	s := loadCalcState()
	s.Error = false
	digitStr := strconv.Itoa(d)

	if s.NewInput {
		s.Display = digitStr
		s.NewInput = false
	} else {
		if s.Display == "0" {
			s.Display = digitStr
		} else if len(s.Display) < 12 {
			s.Display += digitStr
		}
	}

	saveCalcState(s)
	updateCalcDisplay(s)
}

func calcOperator(op string) {
	s := loadCalcState()
	s.Error = false

	current, err := strconv.ParseFloat(s.Display, 64)
	if err != nil {
		current = 0
	}

	if s.Operator != nil && !s.NewInput && s.Operand1 != nil {
		res, ok := doCalculate(*s.Operand1, *s.Operator, current)
		if !ok {
			s = defaultCalcState()
			s.Display = "ERR"
			s.Error = true
			saveCalcState(s)
			updateCalcDisplay(s)
			return
		}
		s.Display = formatCalcNumber(res)
		current = res
	}

	op1 := current
	opCopy := op
	s.Operand1 = &op1
	s.Operator = &opCopy
	s.NewInput = true

	saveCalcState(s)
	updateCalcDisplay(s)
}

func calcEquals() {
	s := loadCalcState()

	if s.Operator != nil && s.Operand1 != nil {
		current, err := strconv.ParseFloat(s.Display, 64)
		if err != nil {
			current = 0
		}
		res, ok := doCalculate(*s.Operand1, *s.Operator, current)
		if !ok {
			s = defaultCalcState()
			s.Display = "ERR"
			s.Error = true
		} else {
			s.Display = formatCalcNumber(res)
			s.Operand1 = nil
			s.Operator = nil
			s.NewInput = true
			s.Error = false
		}
	} else {
		s.NewInput = true
	}

	saveCalcState(s)
	updateCalcDisplay(s)
}

func calcClear() {
	s := defaultCalcState()
	saveCalcState(s)
	updateCalcDisplay(s)
}

// RunCalc executes the calculator plugin CLI:
//
//	calc {init|digit <n>|op <+|-|*|/>|equals|clear}
func RunCalc(args []string) int {
	if len(args) < 1 {
		fmt.Println("Usage: deckctl calc {init|digit <n>|op <+|-|*|/>|equals|clear}")
		return 1
	}

	switch args[0] {
	case "init":
		calcInit()
	case "digit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl calc digit <n>")
			return 1
		}
		d, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid digit: %s\n", args[1])
			return 1
		}
		calcDigit(d)
	case "op":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl calc op <+|-|*|/>")
			return 1
		}
		calcOperator(args[1])
	case "equals":
		calcEquals()
	case "clear":
		calcClear()
	default:
		fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[0])
		return 1
	}
	return 0
}
