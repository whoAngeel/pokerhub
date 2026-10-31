package engine

import "testing"

func evalAll(cards []Card) int32 { return defaultEvaluator.Evaluate(cards) }

func TestEvaluatorCategoryOrdering(t *testing.T) {
	// Each hand is a distinct category, ordered best to worst. Evaluate
	// should be strictly increasing (lower = better) across categories.
	hands := []struct {
		name  string
		cards []Card
	}{
		{"straight_flush", cs("9s", "Ts", "Js", "Qs", "Ks")},
		{"four_of_a_kind", cs("9s", "9h", "9d", "9c", "Kh")},
		{"full_house", cs("9s", "9h", "9d", "Kc", "Kh")},
		{"flush", cs("2s", "5s", "7s", "9s", "Ks")},
		{"straight", cs("9s", "Th", "Jd", "Qc", "Kh")},
		{"three_of_a_kind", cs("9s", "9h", "9d", "2c", "Kh")},
		{"two_pair", cs("9s", "9h", "2d", "2c", "Kh")},
		{"pair", cs("9s", "9h", "2d", "4c", "Kh")},
		{"high_card", cs("2s", "5h", "7d", "9c", "Kh")},
	}
	var prev int32 = -1
	var prevClass int32
	for _, hand := range hands {
		code := evalAll(hand.cards)
		class := defaultEvaluator.Class(code)
		if code <= prev {
			t.Errorf("%s: code %d should be worse (higher) than previous %d", hand.name, code, prev)
		}
		if class <= prevClass {
			t.Errorf("%s: class %d should be worse (higher) than previous class %d", hand.name, class, prevClass)
		}
		prev = code
		prevClass = class
	}
}

func TestEvaluatorKickers(t *testing.T) {
	// Same category (pair of nines), better kicker should win (lower code).
	better := evalAll(cs("9s", "9h", "Ac", "4d", "2c"))
	worse := evalAll(cs("9s", "9h", "Kc", "4d", "2c"))
	if !(better < worse) {
		t.Errorf("A-kicker pair of 9s (%d) should beat K-kicker pair of 9s (%d)", better, worse)
	}
}

func TestEvaluatorWheelStraight(t *testing.T) {
	wheel := evalAll(cs("Ah", "2s", "3d", "4c", "5h"))
	sixHigh := evalAll(cs("2h", "3s", "4d", "5c", "6h"))
	if defaultEvaluator.Class(wheel) != 5 {
		t.Fatalf("wheel should be class 5 (straight), got %d", defaultEvaluator.Class(wheel))
	}
	if defaultEvaluator.Class(sixHigh) != 5 {
		t.Fatalf("6-high straight should be class 5, got %d", defaultEvaluator.Class(sixHigh))
	}
	if !(sixHigh < wheel) {
		t.Errorf("6-high straight (%d) should beat the wheel (%d)", sixHigh, wheel)
	}
}

func TestEvaluatorBoardPlaysSplit(t *testing.T) {
	// Board itself is a broadway straight; neither hole pair improves it.
	board := cs("Ah", "Kd", "Qs", "Jc", "Th")
	p1 := evaluate7(cs("2c", "3c"), board)
	p2 := evaluate7(cs("4d", "5d"), board)
	if p1 != p2 {
		t.Errorf("both hands should play the board and tie: p1=%d p2=%d", p1, p2)
	}
	if defaultEvaluator.Class(p1) != 5 {
		t.Fatalf("expected straight, got class %d", defaultEvaluator.Class(p1))
	}
}

func TestEvaluatorCounterfeitTwoPair(t *testing.T) {
	// Board has two pair (A,K); a pocket pair of Qs is counterfeited and
	// only serves as a kicker, same as any other non-pairing kicker
	// would, so the better kicker still wins between two counterfeited
	// hands.
	board := cs("Ah", "As", "Kd", "Kc", "2h")
	withQQ := evaluate7(cs("Qs", "Qh"), board) // kicker Q
	withJ9 := evaluate7(cs("Jd", "9c"), board) // kicker J

	if defaultEvaluator.Class(withQQ) != 7 || defaultEvaluator.Class(withJ9) != 7 {
		t.Fatalf("expected both to be two pair (class 7), got %d and %d",
			defaultEvaluator.Class(withQQ), defaultEvaluator.Class(withJ9))
	}
	if !(withQQ < withJ9) {
		t.Errorf("Q-kicker two pair (%d) should beat J-kicker two pair (%d)", withQQ, withJ9)
	}
}
