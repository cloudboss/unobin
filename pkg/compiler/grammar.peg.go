package compiler

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

const endSymbol rune = 1114112

/* The rule types inferred from the grammar are below. */
type pegRule uint8

const (
	ruleUnknown pegRule = iota
	ruleentry
	ruleident
	ruleident_chars
	ruleident_char
	rulecomment
	rulespace
	rulenot_newline
	rulenewline
	ruleall_space
	rulealpha
	ruledigit
	rulesentence_char
	rulesentence
	rulestatement
	rulepair
	rulevalue
	ruleindex_expr
	ruleindex_expr_tail
	rulearray
	rulebool_expr
	ruleitem_delim
	rulefun_expr
	rulemath_expr
	rulenumber
	ruleint
	rulefloat
	ruleobject
	rulestring
	ruleblock
	rulesimple_block
	ruleblock_description
	rulecompound_block
	rulerescue_clause
	rulealways_clause
	ruleend
	rulePAIR_DELIM
	ruleDOT
	ruleSTRING_DELIM
	ruleCOMMENT_START
	ruleMINUS
	ruleINDEX_OPEN
	ruleINDEX_CLOSE
	ruleARRAY_OPEN
	ruleARRAY_CLOSE
	ruleGROUP_OPEN
	ruleGROUP_CLOSE
	ruleBLOCK_OPEN
	ruleBLOCK_CLOSE
	ruleBLOCK
	ruleRESCUE
	ruleALWAYS
	ruleTRUE
	ruleFALSE
	ruleEQ
	ruleLT
	ruleGT
	ruleLTE
	ruleGTE
	ruleAND
	ruleOR

	rulePre
	ruleIn
	ruleSuf
)

var rul3s = [...]string{
	"Unknown",
	"entry",
	"ident",
	"ident_chars",
	"ident_char",
	"comment",
	"space",
	"not_newline",
	"newline",
	"all_space",
	"alpha",
	"digit",
	"sentence_char",
	"sentence",
	"statement",
	"pair",
	"value",
	"index_expr",
	"index_expr_tail",
	"array",
	"bool_expr",
	"item_delim",
	"fun_expr",
	"math_expr",
	"number",
	"int",
	"float",
	"object",
	"string",
	"block",
	"simple_block",
	"block_description",
	"compound_block",
	"rescue_clause",
	"always_clause",
	"end",
	"PAIR_DELIM",
	"DOT",
	"STRING_DELIM",
	"COMMENT_START",
	"MINUS",
	"INDEX_OPEN",
	"INDEX_CLOSE",
	"ARRAY_OPEN",
	"ARRAY_CLOSE",
	"GROUP_OPEN",
	"GROUP_CLOSE",
	"BLOCK_OPEN",
	"BLOCK_CLOSE",
	"BLOCK",
	"RESCUE",
	"ALWAYS",
	"TRUE",
	"FALSE",
	"EQ",
	"LT",
	"GT",
	"LTE",
	"GTE",
	"AND",
	"OR",

	"Pre_",
	"_In_",
	"_Suf",
}

type node32 struct {
	token32
	up, next *node32
}

func (node *node32) print(depth int, buffer string) {
	for node != nil {
		for c := 0; c < depth; c++ {
			fmt.Printf(" ")
		}
		fmt.Printf("\x1B[34m%v\x1B[m %v\n", rul3s[node.pegRule], strconv.Quote(string(([]rune(buffer)[node.begin:node.end]))))
		if node.up != nil {
			node.up.print(depth+1, buffer)
		}
		node = node.next
	}
}

func (node *node32) Print(buffer string) {
	node.print(0, buffer)
}

type element struct {
	node *node32
	down *element
}

/* ${@} bit structure for abstract syntax tree */
type token32 struct {
	pegRule
	begin, end, next uint32
}

func (t *token32) isZero() bool {
	return t.pegRule == ruleUnknown && t.begin == 0 && t.end == 0 && t.next == 0
}

func (t *token32) isParentOf(u token32) bool {
	return t.begin <= u.begin && t.end >= u.end && t.next > u.next
}

func (t *token32) getToken32() token32 {
	return token32{pegRule: t.pegRule, begin: uint32(t.begin), end: uint32(t.end), next: uint32(t.next)}
}

func (t *token32) String() string {
	return fmt.Sprintf("\x1B[34m%v\x1B[m %v %v %v", rul3s[t.pegRule], t.begin, t.end, t.next)
}

type tokens32 struct {
	tree    []token32
	ordered [][]token32
}

func (t *tokens32) trim(length int) {
	t.tree = t.tree[0:length]
}

func (t *tokens32) Print() {
	for _, token := range t.tree {
		fmt.Println(token.String())
	}
}

func (t *tokens32) Order() [][]token32 {
	if t.ordered != nil {
		return t.ordered
	}

	depths := make([]int32, 1, math.MaxInt16)
	for i, token := range t.tree {
		if token.pegRule == ruleUnknown {
			t.tree = t.tree[:i]
			break
		}
		depth := int(token.next)
		if length := len(depths); depth >= length {
			depths = depths[:depth+1]
		}
		depths[depth]++
	}
	depths = append(depths, 0)

	ordered, pool := make([][]token32, len(depths)), make([]token32, len(t.tree)+len(depths))
	for i, depth := range depths {
		depth++
		ordered[i], pool, depths[i] = pool[:depth], pool[depth:], 0
	}

	for i, token := range t.tree {
		depth := token.next
		token.next = uint32(i)
		ordered[depth][depths[depth]] = token
		depths[depth]++
	}
	t.ordered = ordered
	return ordered
}

type state32 struct {
	token32
	depths []int32
	leaf   bool
}

func (t *tokens32) AST() *node32 {
	tokens := t.Tokens()
	stack := &element{node: &node32{token32: <-tokens}}
	for token := range tokens {
		if token.begin == token.end {
			continue
		}
		node := &node32{token32: token}
		for stack != nil && stack.node.begin >= token.begin && stack.node.end <= token.end {
			stack.node.next = node.up
			node.up = stack.node
			stack = stack.down
		}
		stack = &element{node: node, down: stack}
	}
	return stack.node
}

func (t *tokens32) PreOrder() (<-chan state32, [][]token32) {
	s, ordered := make(chan state32, 6), t.Order()
	go func() {
		var states [8]state32
		for i := range states {
			states[i].depths = make([]int32, len(ordered))
		}
		depths, state, depth := make([]int32, len(ordered)), 0, 1
		write := func(t token32, leaf bool) {
			S := states[state]
			state, S.pegRule, S.begin, S.end, S.next, S.leaf = (state+1)%8, t.pegRule, t.begin, t.end, uint32(depth), leaf
			copy(S.depths, depths)
			s <- S
		}

		states[state].token32 = ordered[0][0]
		depths[0]++
		state++
		a, b := ordered[depth-1][depths[depth-1]-1], ordered[depth][depths[depth]]
	depthFirstSearch:
		for {
			for {
				if i := depths[depth]; i > 0 {
					if c, j := ordered[depth][i-1], depths[depth-1]; a.isParentOf(c) &&
						(j < 2 || !ordered[depth-1][j-2].isParentOf(c)) {
						if c.end != b.begin {
							write(token32{pegRule: ruleIn, begin: c.end, end: b.begin}, true)
						}
						break
					}
				}

				if a.begin < b.begin {
					write(token32{pegRule: rulePre, begin: a.begin, end: b.begin}, true)
				}
				break
			}

			next := depth + 1
			if c := ordered[next][depths[next]]; c.pegRule != ruleUnknown && b.isParentOf(c) {
				write(b, false)
				depths[depth]++
				depth, a, b = next, b, c
				continue
			}

			write(b, true)
			depths[depth]++
			c, parent := ordered[depth][depths[depth]], true
			for {
				if c.pegRule != ruleUnknown && a.isParentOf(c) {
					b = c
					continue depthFirstSearch
				} else if parent && b.end != a.end {
					write(token32{pegRule: ruleSuf, begin: b.end, end: a.end}, true)
				}

				depth--
				if depth > 0 {
					a, b, c = ordered[depth-1][depths[depth-1]-1], a, ordered[depth][depths[depth]]
					parent = a.isParentOf(b)
					continue
				}

				break depthFirstSearch
			}
		}

		close(s)
	}()
	return s, ordered
}

func (t *tokens32) PrintSyntax() {
	tokens, ordered := t.PreOrder()
	max := -1
	for token := range tokens {
		if !token.leaf {
			fmt.Printf("%v", token.begin)
			for i, leaf, depths := 0, int(token.next), token.depths; i < leaf; i++ {
				fmt.Printf(" \x1B[36m%v\x1B[m", rul3s[ordered[i][depths[i]-1].pegRule])
			}
			fmt.Printf(" \x1B[36m%v\x1B[m\n", rul3s[token.pegRule])
		} else if token.begin == token.end {
			fmt.Printf("%v", token.begin)
			for i, leaf, depths := 0, int(token.next), token.depths; i < leaf; i++ {
				fmt.Printf(" \x1B[31m%v\x1B[m", rul3s[ordered[i][depths[i]-1].pegRule])
			}
			fmt.Printf(" \x1B[31m%v\x1B[m\n", rul3s[token.pegRule])
		} else {
			for c, end := token.begin, token.end; c < end; c++ {
				if i := int(c); max+1 < i {
					for j := max; j < i; j++ {
						fmt.Printf("skip %v %v\n", j, token.String())
					}
					max = i
				} else if i := int(c); i <= max {
					for j := i; j <= max; j++ {
						fmt.Printf("dupe %v %v\n", j, token.String())
					}
				} else {
					max = int(c)
				}
				fmt.Printf("%v", c)
				for i, leaf, depths := 0, int(token.next), token.depths; i < leaf; i++ {
					fmt.Printf(" \x1B[34m%v\x1B[m", rul3s[ordered[i][depths[i]-1].pegRule])
				}
				fmt.Printf(" \x1B[34m%v\x1B[m\n", rul3s[token.pegRule])
			}
			fmt.Printf("\n")
		}
	}
}

func (t *tokens32) PrintSyntaxTree(buffer string) {
	tokens, _ := t.PreOrder()
	for token := range tokens {
		for c := 0; c < int(token.next); c++ {
			fmt.Printf(" ")
		}
		fmt.Printf("\x1B[34m%v\x1B[m %v\n", rul3s[token.pegRule], strconv.Quote(string(([]rune(buffer)[token.begin:token.end]))))
	}
}

func (t *tokens32) Add(rule pegRule, begin, end, depth uint32, index int) {
	t.tree[index] = token32{pegRule: rule, begin: uint32(begin), end: uint32(end), next: uint32(depth)}
}

func (t *tokens32) Tokens() <-chan token32 {
	s := make(chan token32, 16)
	go func() {
		for _, v := range t.tree {
			s <- v.getToken32()
		}
		close(s)
	}()
	return s
}

func (t *tokens32) Error() []token32 {
	ordered := t.Order()
	length := len(ordered)
	tokens, length := make([]token32, length), length-1
	for i := range tokens {
		o := ordered[length-i]
		if len(o) > 1 {
			tokens[i] = o[len(o)-2].getToken32()
		}
	}
	return tokens
}

func (t *tokens32) Expand(index int) {
	tree := t.tree
	if index >= len(tree) {
		expanded := make([]token32, 2*len(tree))
		copy(expanded, tree)
		t.tree = expanded
	}
}

type Grammar struct {
	Buffer string
	buffer []rune
	rules  [61]func() bool
	Parse  func(rule ...int) error
	Reset  func()
	Pretty bool
	tokens32
}

type textPosition struct {
	line, symbol int
}

type textPositionMap map[int]textPosition

func translatePositions(buffer []rune, positions []int) textPositionMap {
	length, translations, j, line, symbol := len(positions), make(textPositionMap, len(positions)), 0, 1, 0
	sort.Ints(positions)

search:
	for i, c := range buffer {
		if c == '\n' {
			line, symbol = line+1, 0
		} else {
			symbol++
		}
		if i == positions[j] {
			translations[positions[j]] = textPosition{line, symbol}
			for j++; j < length; j++ {
				if i != positions[j] {
					continue search
				}
			}
			break search
		}
	}

	return translations
}

type parseError struct {
	p   *Grammar
	max token32
}

func (e *parseError) Error() string {
	tokens, error := []token32{e.max}, "\n"
	positions, p := make([]int, 2*len(tokens)), 0
	for _, token := range tokens {
		positions[p], p = int(token.begin), p+1
		positions[p], p = int(token.end), p+1
	}
	translations := translatePositions(e.p.buffer, positions)
	format := "parse error near %v (line %v symbol %v - line %v symbol %v):\n%v\n"
	if e.p.Pretty {
		format = "parse error near \x1B[34m%v\x1B[m (line %v symbol %v - line %v symbol %v):\n%v\n"
	}
	for _, token := range tokens {
		begin, end := int(token.begin), int(token.end)
		error += fmt.Sprintf(format,
			rul3s[token.pegRule],
			translations[begin].line, translations[begin].symbol,
			translations[end].line, translations[end].symbol,
			strconv.Quote(string(e.p.buffer[begin:end])))
	}

	return error
}

func (p *Grammar) PrintSyntaxTree() {
	p.tokens32.PrintSyntaxTree(p.Buffer)
}

func (p *Grammar) Highlighter() {
	p.PrintSyntax()
}

func (p *Grammar) Init() {
	p.buffer = []rune(p.Buffer)
	if len(p.buffer) == 0 || p.buffer[len(p.buffer)-1] != endSymbol {
		p.buffer = append(p.buffer, endSymbol)
	}

	tree := tokens32{tree: make([]token32, math.MaxInt16)}
	var max token32
	position, depth, tokenIndex, buffer, _rules := uint32(0), uint32(0), 0, p.buffer, p.rules

	p.Parse = func(rule ...int) error {
		r := 1
		if len(rule) > 0 {
			r = rule[0]
		}
		matches := p.rules[r]()
		p.tokens32 = tree
		if matches {
			p.trim(tokenIndex)
			return nil
		}
		return &parseError{p, max}
	}

	p.Reset = func() {
		position, tokenIndex, depth = 0, 0, 0
	}

	add := func(rule pegRule, begin uint32) {
		tree.Expand(tokenIndex)
		tree.Add(rule, begin, position, depth, tokenIndex)
		tokenIndex++
		if begin != position && position > max.end {
			max = token32{rule, begin, position, depth}
		}
	}

	matchDot := func() bool {
		if buffer[position] != endSymbol {
			position++
			return true
		}
		return false
	}

	/*matchChar := func(c byte) bool {
		if buffer[position] == c {
			position++
			return true
		}
		return false
	}*/

	/*matchRange := func(lower byte, upper byte) bool {
		if c := buffer[position]; c >= lower && c <= upper {
			position++
			return true
		}
		return false
	}*/

	_rules = [...]func() bool{
		nil,
		/* 0 entry <- <(all_space* statement* end)> */
		func() bool {
			position0, tokenIndex0, depth0 := position, tokenIndex, depth
			{
				position1 := position
				depth++
			l2:
				{
					position3, tokenIndex3, depth3 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l3
					}
					goto l2
				l3:
					position, tokenIndex, depth = position3, tokenIndex3, depth3
				}
			l4:
				{
					position5, tokenIndex5, depth5 := position, tokenIndex, depth
					if !_rules[rulestatement]() {
						goto l5
					}
					goto l4
				l5:
					position, tokenIndex, depth = position5, tokenIndex5, depth5
				}
				if !_rules[ruleend]() {
					goto l0
				}
				depth--
				add(ruleentry, position1)
			}
			return true
		l0:
			position, tokenIndex, depth = position0, tokenIndex0, depth0
			return false
		},
		/* 1 ident <- <(ident_chars space*)> */
		func() bool {
			position6, tokenIndex6, depth6 := position, tokenIndex, depth
			{
				position7 := position
				depth++
				if !_rules[ruleident_chars]() {
					goto l6
				}
			l8:
				{
					position9, tokenIndex9, depth9 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l9
					}
					goto l8
				l9:
					position, tokenIndex, depth = position9, tokenIndex9, depth9
				}
				depth--
				add(ruleident, position7)
			}
			return true
		l6:
			position, tokenIndex, depth = position6, tokenIndex6, depth6
			return false
		},
		/* 2 ident_chars <- <(alpha ident_char*)> */
		func() bool {
			position10, tokenIndex10, depth10 := position, tokenIndex, depth
			{
				position11 := position
				depth++
				if !_rules[rulealpha]() {
					goto l10
				}
			l12:
				{
					position13, tokenIndex13, depth13 := position, tokenIndex, depth
					if !_rules[ruleident_char]() {
						goto l13
					}
					goto l12
				l13:
					position, tokenIndex, depth = position13, tokenIndex13, depth13
				}
				depth--
				add(ruleident_chars, position11)
			}
			return true
		l10:
			position, tokenIndex, depth = position10, tokenIndex10, depth10
			return false
		},
		/* 3 ident_char <- <(alpha / digit / MINUS)> */
		func() bool {
			position14, tokenIndex14, depth14 := position, tokenIndex, depth
			{
				position15 := position
				depth++
				{
					position16, tokenIndex16, depth16 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l17
					}
					goto l16
				l17:
					position, tokenIndex, depth = position16, tokenIndex16, depth16
					if !_rules[ruledigit]() {
						goto l18
					}
					goto l16
				l18:
					position, tokenIndex, depth = position16, tokenIndex16, depth16
					if !_rules[ruleMINUS]() {
						goto l14
					}
				}
			l16:
				depth--
				add(ruleident_char, position15)
			}
			return true
		l14:
			position, tokenIndex, depth = position14, tokenIndex14, depth14
			return false
		},
		/* 4 comment <- <(COMMENT_START not_newline* (newline / end))> */
		func() bool {
			position19, tokenIndex19, depth19 := position, tokenIndex, depth
			{
				position20 := position
				depth++
				if !_rules[ruleCOMMENT_START]() {
					goto l19
				}
			l21:
				{
					position22, tokenIndex22, depth22 := position, tokenIndex, depth
					if !_rules[rulenot_newline]() {
						goto l22
					}
					goto l21
				l22:
					position, tokenIndex, depth = position22, tokenIndex22, depth22
				}
				{
					position23, tokenIndex23, depth23 := position, tokenIndex, depth
					if !_rules[rulenewline]() {
						goto l24
					}
					goto l23
				l24:
					position, tokenIndex, depth = position23, tokenIndex23, depth23
					if !_rules[ruleend]() {
						goto l19
					}
				}
			l23:
				depth--
				add(rulecomment, position20)
			}
			return true
		l19:
			position, tokenIndex, depth = position19, tokenIndex19, depth19
			return false
		},
		/* 5 space <- <(' ' / '\t')> */
		func() bool {
			position25, tokenIndex25, depth25 := position, tokenIndex, depth
			{
				position26 := position
				depth++
				{
					position27, tokenIndex27, depth27 := position, tokenIndex, depth
					if buffer[position] != rune(' ') {
						goto l28
					}
					position++
					goto l27
				l28:
					position, tokenIndex, depth = position27, tokenIndex27, depth27
					if buffer[position] != rune('\t') {
						goto l25
					}
					position++
				}
			l27:
				depth--
				add(rulespace, position26)
			}
			return true
		l25:
			position, tokenIndex, depth = position25, tokenIndex25, depth25
			return false
		},
		/* 6 not_newline <- <(!('\r' / '\n') .)> */
		func() bool {
			position29, tokenIndex29, depth29 := position, tokenIndex, depth
			{
				position30 := position
				depth++
				{
					position31, tokenIndex31, depth31 := position, tokenIndex, depth
					{
						position32, tokenIndex32, depth32 := position, tokenIndex, depth
						if buffer[position] != rune('\r') {
							goto l33
						}
						position++
						goto l32
					l33:
						position, tokenIndex, depth = position32, tokenIndex32, depth32
						if buffer[position] != rune('\n') {
							goto l31
						}
						position++
					}
				l32:
					goto l29
				l31:
					position, tokenIndex, depth = position31, tokenIndex31, depth31
				}
				if !matchDot() {
					goto l29
				}
				depth--
				add(rulenot_newline, position30)
			}
			return true
		l29:
			position, tokenIndex, depth = position29, tokenIndex29, depth29
			return false
		},
		/* 7 newline <- <('\r' / '\n')> */
		func() bool {
			position34, tokenIndex34, depth34 := position, tokenIndex, depth
			{
				position35 := position
				depth++
				{
					position36, tokenIndex36, depth36 := position, tokenIndex, depth
					if buffer[position] != rune('\r') {
						goto l37
					}
					position++
					goto l36
				l37:
					position, tokenIndex, depth = position36, tokenIndex36, depth36
					if buffer[position] != rune('\n') {
						goto l34
					}
					position++
				}
			l36:
				depth--
				add(rulenewline, position35)
			}
			return true
		l34:
			position, tokenIndex, depth = position34, tokenIndex34, depth34
			return false
		},
		/* 8 all_space <- <(space / newline / comment)> */
		func() bool {
			position38, tokenIndex38, depth38 := position, tokenIndex, depth
			{
				position39 := position
				depth++
				{
					position40, tokenIndex40, depth40 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l41
					}
					goto l40
				l41:
					position, tokenIndex, depth = position40, tokenIndex40, depth40
					if !_rules[rulenewline]() {
						goto l42
					}
					goto l40
				l42:
					position, tokenIndex, depth = position40, tokenIndex40, depth40
					if !_rules[rulecomment]() {
						goto l38
					}
				}
			l40:
				depth--
				add(ruleall_space, position39)
			}
			return true
		l38:
			position, tokenIndex, depth = position38, tokenIndex38, depth38
			return false
		},
		/* 9 alpha <- <([A-Z] / [a-z])> */
		func() bool {
			position43, tokenIndex43, depth43 := position, tokenIndex, depth
			{
				position44 := position
				depth++
				{
					position45, tokenIndex45, depth45 := position, tokenIndex, depth
					if c := buffer[position]; c < rune('A') || c > rune('Z') {
						goto l46
					}
					position++
					goto l45
				l46:
					position, tokenIndex, depth = position45, tokenIndex45, depth45
					if c := buffer[position]; c < rune('a') || c > rune('z') {
						goto l43
					}
					position++
				}
			l45:
				depth--
				add(rulealpha, position44)
			}
			return true
		l43:
			position, tokenIndex, depth = position43, tokenIndex43, depth43
			return false
		},
		/* 10 digit <- <[0-9]> */
		func() bool {
			position47, tokenIndex47, depth47 := position, tokenIndex, depth
			{
				position48 := position
				depth++
				if c := buffer[position]; c < rune('0') || c > rune('9') {
					goto l47
				}
				position++
				depth--
				add(ruledigit, position48)
			}
			return true
		l47:
			position, tokenIndex, depth = position47, tokenIndex47, depth47
			return false
		},
		/* 11 sentence_char <- <(!(INDEX_CLOSE / all_space) .)> */
		func() bool {
			position49, tokenIndex49, depth49 := position, tokenIndex, depth
			{
				position50 := position
				depth++
				{
					position51, tokenIndex51, depth51 := position, tokenIndex, depth
					{
						position52, tokenIndex52, depth52 := position, tokenIndex, depth
						if !_rules[ruleINDEX_CLOSE]() {
							goto l53
						}
						goto l52
					l53:
						position, tokenIndex, depth = position52, tokenIndex52, depth52
						if !_rules[ruleall_space]() {
							goto l51
						}
					}
				l52:
					goto l49
				l51:
					position, tokenIndex, depth = position51, tokenIndex51, depth51
				}
				if !matchDot() {
					goto l49
				}
				depth--
				add(rulesentence_char, position50)
			}
			return true
		l49:
			position, tokenIndex, depth = position49, tokenIndex49, depth49
			return false
		},
		/* 12 sentence <- <(sentence_char+ (space+ sentence_char+)*)> */
		func() bool {
			position54, tokenIndex54, depth54 := position, tokenIndex, depth
			{
				position55 := position
				depth++
				if !_rules[rulesentence_char]() {
					goto l54
				}
			l56:
				{
					position57, tokenIndex57, depth57 := position, tokenIndex, depth
					if !_rules[rulesentence_char]() {
						goto l57
					}
					goto l56
				l57:
					position, tokenIndex, depth = position57, tokenIndex57, depth57
				}
			l58:
				{
					position59, tokenIndex59, depth59 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l59
					}
				l60:
					{
						position61, tokenIndex61, depth61 := position, tokenIndex, depth
						if !_rules[rulespace]() {
							goto l61
						}
						goto l60
					l61:
						position, tokenIndex, depth = position61, tokenIndex61, depth61
					}
					if !_rules[rulesentence_char]() {
						goto l59
					}
				l62:
					{
						position63, tokenIndex63, depth63 := position, tokenIndex, depth
						if !_rules[rulesentence_char]() {
							goto l63
						}
						goto l62
					l63:
						position, tokenIndex, depth = position63, tokenIndex63, depth63
					}
					goto l58
				l59:
					position, tokenIndex, depth = position59, tokenIndex59, depth59
				}
				depth--
				add(rulesentence, position55)
			}
			return true
		l54:
			position, tokenIndex, depth = position54, tokenIndex54, depth54
			return false
		},
		/* 13 statement <- <(pair / block)> */
		func() bool {
			position64, tokenIndex64, depth64 := position, tokenIndex, depth
			{
				position65 := position
				depth++
				{
					position66, tokenIndex66, depth66 := position, tokenIndex, depth
					if !_rules[rulepair]() {
						goto l67
					}
					goto l66
				l67:
					position, tokenIndex, depth = position66, tokenIndex66, depth66
					if !_rules[ruleblock]() {
						goto l64
					}
				}
			l66:
				depth--
				add(rulestatement, position65)
			}
			return true
		l64:
			position, tokenIndex, depth = position64, tokenIndex64, depth64
			return false
		},
		/* 14 pair <- <((ident / string) PAIR_DELIM value all_space*)> */
		func() bool {
			position68, tokenIndex68, depth68 := position, tokenIndex, depth
			{
				position69 := position
				depth++
				{
					position70, tokenIndex70, depth70 := position, tokenIndex, depth
					if !_rules[ruleident]() {
						goto l71
					}
					goto l70
				l71:
					position, tokenIndex, depth = position70, tokenIndex70, depth70
					if !_rules[rulestring]() {
						goto l68
					}
				}
			l70:
				if !_rules[rulePAIR_DELIM]() {
					goto l68
				}
				if !_rules[rulevalue]() {
					goto l68
				}
			l72:
				{
					position73, tokenIndex73, depth73 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l73
					}
					goto l72
				l73:
					position, tokenIndex, depth = position73, tokenIndex73, depth73
				}
				depth--
				add(rulepair, position69)
			}
			return true
		l68:
			position, tokenIndex, depth = position68, tokenIndex68, depth68
			return false
		},
		/* 15 value <- <(array / bool_expr / fun_expr / index_expr / math_expr / object / string)> */
		func() bool {
			position74, tokenIndex74, depth74 := position, tokenIndex, depth
			{
				position75 := position
				depth++
				{
					position76, tokenIndex76, depth76 := position, tokenIndex, depth
					if !_rules[rulearray]() {
						goto l77
					}
					goto l76
				l77:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[rulebool_expr]() {
						goto l78
					}
					goto l76
				l78:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[rulefun_expr]() {
						goto l79
					}
					goto l76
				l79:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[ruleindex_expr]() {
						goto l80
					}
					goto l76
				l80:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[rulemath_expr]() {
						goto l81
					}
					goto l76
				l81:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[ruleobject]() {
						goto l82
					}
					goto l76
				l82:
					position, tokenIndex, depth = position76, tokenIndex76, depth76
					if !_rules[rulestring]() {
						goto l74
					}
				}
			l76:
				depth--
				add(rulevalue, position75)
			}
			return true
		l74:
			position, tokenIndex, depth = position74, tokenIndex74, depth74
			return false
		},
		/* 16 index_expr <- <(ident_chars index_expr_tail+)> */
		func() bool {
			position83, tokenIndex83, depth83 := position, tokenIndex, depth
			{
				position84 := position
				depth++
				if !_rules[ruleident_chars]() {
					goto l83
				}
				if !_rules[ruleindex_expr_tail]() {
					goto l83
				}
			l85:
				{
					position86, tokenIndex86, depth86 := position, tokenIndex, depth
					if !_rules[ruleindex_expr_tail]() {
						goto l86
					}
					goto l85
				l86:
					position, tokenIndex, depth = position86, tokenIndex86, depth86
				}
				depth--
				add(ruleindex_expr, position84)
			}
			return true
		l83:
			position, tokenIndex, depth = position83, tokenIndex83, depth83
			return false
		},
		/* 17 index_expr_tail <- <((DOT ident_chars) / (INDEX_OPEN sentence INDEX_CLOSE))> */
		func() bool {
			position87, tokenIndex87, depth87 := position, tokenIndex, depth
			{
				position88 := position
				depth++
				{
					position89, tokenIndex89, depth89 := position, tokenIndex, depth
					if !_rules[ruleDOT]() {
						goto l90
					}
					if !_rules[ruleident_chars]() {
						goto l90
					}
					goto l89
				l90:
					position, tokenIndex, depth = position89, tokenIndex89, depth89
					if !_rules[ruleINDEX_OPEN]() {
						goto l87
					}
					if !_rules[rulesentence]() {
						goto l87
					}
					if !_rules[ruleINDEX_CLOSE]() {
						goto l87
					}
				}
			l89:
				depth--
				add(ruleindex_expr_tail, position88)
			}
			return true
		l87:
			position, tokenIndex, depth = position87, tokenIndex87, depth87
			return false
		},
		/* 18 array <- <(ARRAY_OPEN (value (item_delim value)*)* item_delim ARRAY_CLOSE)> */
		func() bool {
			position91, tokenIndex91, depth91 := position, tokenIndex, depth
			{
				position92 := position
				depth++
				if !_rules[ruleARRAY_OPEN]() {
					goto l91
				}
			l93:
				{
					position94, tokenIndex94, depth94 := position, tokenIndex, depth
					if !_rules[rulevalue]() {
						goto l94
					}
				l95:
					{
						position96, tokenIndex96, depth96 := position, tokenIndex, depth
						if !_rules[ruleitem_delim]() {
							goto l96
						}
						if !_rules[rulevalue]() {
							goto l96
						}
						goto l95
					l96:
						position, tokenIndex, depth = position96, tokenIndex96, depth96
					}
					goto l93
				l94:
					position, tokenIndex, depth = position94, tokenIndex94, depth94
				}
				if !_rules[ruleitem_delim]() {
					goto l91
				}
				if !_rules[ruleARRAY_CLOSE]() {
					goto l91
				}
				depth--
				add(rulearray, position92)
			}
			return true
		l91:
			position, tokenIndex, depth = position91, tokenIndex91, depth91
			return false
		},
		/* 19 bool_expr <- <((TRUE / FALSE) all_space*)> */
		func() bool {
			position97, tokenIndex97, depth97 := position, tokenIndex, depth
			{
				position98 := position
				depth++
				{
					position99, tokenIndex99, depth99 := position, tokenIndex, depth
					if !_rules[ruleTRUE]() {
						goto l100
					}
					goto l99
				l100:
					position, tokenIndex, depth = position99, tokenIndex99, depth99
					if !_rules[ruleFALSE]() {
						goto l97
					}
				}
			l99:
			l101:
				{
					position102, tokenIndex102, depth102 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l102
					}
					goto l101
				l102:
					position, tokenIndex, depth = position102, tokenIndex102, depth102
				}
				depth--
				add(rulebool_expr, position98)
			}
			return true
		l97:
			position, tokenIndex, depth = position97, tokenIndex97, depth97
			return false
		},
		/* 20 item_delim <- <all_space*> */
		func() bool {
			{
				position104 := position
				depth++
			l105:
				{
					position106, tokenIndex106, depth106 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l106
					}
					goto l105
				l106:
					position, tokenIndex, depth = position106, tokenIndex106, depth106
				}
				depth--
				add(ruleitem_delim, position104)
			}
			return true
		},
		/* 21 fun_expr <- <(ident GROUP_OPEN (value (item_delim value)*)* item_delim GROUP_CLOSE all_space*)> */
		func() bool {
			position107, tokenIndex107, depth107 := position, tokenIndex, depth
			{
				position108 := position
				depth++
				if !_rules[ruleident]() {
					goto l107
				}
				if !_rules[ruleGROUP_OPEN]() {
					goto l107
				}
			l109:
				{
					position110, tokenIndex110, depth110 := position, tokenIndex, depth
					if !_rules[rulevalue]() {
						goto l110
					}
				l111:
					{
						position112, tokenIndex112, depth112 := position, tokenIndex, depth
						if !_rules[ruleitem_delim]() {
							goto l112
						}
						if !_rules[rulevalue]() {
							goto l112
						}
						goto l111
					l112:
						position, tokenIndex, depth = position112, tokenIndex112, depth112
					}
					goto l109
				l110:
					position, tokenIndex, depth = position110, tokenIndex110, depth110
				}
				if !_rules[ruleitem_delim]() {
					goto l107
				}
				if !_rules[ruleGROUP_CLOSE]() {
					goto l107
				}
			l113:
				{
					position114, tokenIndex114, depth114 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l114
					}
					goto l113
				l114:
					position, tokenIndex, depth = position114, tokenIndex114, depth114
				}
				depth--
				add(rulefun_expr, position108)
			}
			return true
		l107:
			position, tokenIndex, depth = position107, tokenIndex107, depth107
			return false
		},
		/* 22 math_expr <- <(number space*)> */
		func() bool {
			position115, tokenIndex115, depth115 := position, tokenIndex, depth
			{
				position116 := position
				depth++
				if !_rules[rulenumber]() {
					goto l115
				}
			l117:
				{
					position118, tokenIndex118, depth118 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l118
					}
					goto l117
				l118:
					position, tokenIndex, depth = position118, tokenIndex118, depth118
				}
				depth--
				add(rulemath_expr, position116)
			}
			return true
		l115:
			position, tokenIndex, depth = position115, tokenIndex115, depth115
			return false
		},
		/* 23 number <- <(float / int)> */
		func() bool {
			position119, tokenIndex119, depth119 := position, tokenIndex, depth
			{
				position120 := position
				depth++
				{
					position121, tokenIndex121, depth121 := position, tokenIndex, depth
					if !_rules[rulefloat]() {
						goto l122
					}
					goto l121
				l122:
					position, tokenIndex, depth = position121, tokenIndex121, depth121
					if !_rules[ruleint]() {
						goto l119
					}
				}
			l121:
				depth--
				add(rulenumber, position120)
			}
			return true
		l119:
			position, tokenIndex, depth = position119, tokenIndex119, depth119
			return false
		},
		/* 24 int <- <((MINUS digit+) / digit+)> */
		func() bool {
			position123, tokenIndex123, depth123 := position, tokenIndex, depth
			{
				position124 := position
				depth++
				{
					position125, tokenIndex125, depth125 := position, tokenIndex, depth
					if !_rules[ruleMINUS]() {
						goto l126
					}
					if !_rules[ruledigit]() {
						goto l126
					}
				l127:
					{
						position128, tokenIndex128, depth128 := position, tokenIndex, depth
						if !_rules[ruledigit]() {
							goto l128
						}
						goto l127
					l128:
						position, tokenIndex, depth = position128, tokenIndex128, depth128
					}
					goto l125
				l126:
					position, tokenIndex, depth = position125, tokenIndex125, depth125
					if !_rules[ruledigit]() {
						goto l123
					}
				l129:
					{
						position130, tokenIndex130, depth130 := position, tokenIndex, depth
						if !_rules[ruledigit]() {
							goto l130
						}
						goto l129
					l130:
						position, tokenIndex, depth = position130, tokenIndex130, depth130
					}
				}
			l125:
				depth--
				add(ruleint, position124)
			}
			return true
		l123:
			position, tokenIndex, depth = position123, tokenIndex123, depth123
			return false
		},
		/* 25 float <- <(((MINUS digit+) / digit+) DOT digit+)> */
		func() bool {
			position131, tokenIndex131, depth131 := position, tokenIndex, depth
			{
				position132 := position
				depth++
				{
					position133, tokenIndex133, depth133 := position, tokenIndex, depth
					if !_rules[ruleMINUS]() {
						goto l134
					}
					if !_rules[ruledigit]() {
						goto l134
					}
				l135:
					{
						position136, tokenIndex136, depth136 := position, tokenIndex, depth
						if !_rules[ruledigit]() {
							goto l136
						}
						goto l135
					l136:
						position, tokenIndex, depth = position136, tokenIndex136, depth136
					}
					goto l133
				l134:
					position, tokenIndex, depth = position133, tokenIndex133, depth133
					if !_rules[ruledigit]() {
						goto l131
					}
				l137:
					{
						position138, tokenIndex138, depth138 := position, tokenIndex, depth
						if !_rules[ruledigit]() {
							goto l138
						}
						goto l137
					l138:
						position, tokenIndex, depth = position138, tokenIndex138, depth138
					}
				}
			l133:
				if !_rules[ruleDOT]() {
					goto l131
				}
				if !_rules[ruledigit]() {
					goto l131
				}
			l139:
				{
					position140, tokenIndex140, depth140 := position, tokenIndex, depth
					if !_rules[ruledigit]() {
						goto l140
					}
					goto l139
				l140:
					position, tokenIndex, depth = position140, tokenIndex140, depth140
				}
				depth--
				add(rulefloat, position132)
			}
			return true
		l131:
			position, tokenIndex, depth = position131, tokenIndex131, depth131
			return false
		},
		/* 26 object <- <(BLOCK_OPEN (pair (item_delim pair)*)* BLOCK_CLOSE)> */
		func() bool {
			position141, tokenIndex141, depth141 := position, tokenIndex, depth
			{
				position142 := position
				depth++
				if !_rules[ruleBLOCK_OPEN]() {
					goto l141
				}
			l143:
				{
					position144, tokenIndex144, depth144 := position, tokenIndex, depth
					if !_rules[rulepair]() {
						goto l144
					}
				l145:
					{
						position146, tokenIndex146, depth146 := position, tokenIndex, depth
						if !_rules[ruleitem_delim]() {
							goto l146
						}
						if !_rules[rulepair]() {
							goto l146
						}
						goto l145
					l146:
						position, tokenIndex, depth = position146, tokenIndex146, depth146
					}
					goto l143
				l144:
					position, tokenIndex, depth = position144, tokenIndex144, depth144
				}
				if !_rules[ruleBLOCK_CLOSE]() {
					goto l141
				}
				depth--
				add(ruleobject, position142)
			}
			return true
		l141:
			position, tokenIndex, depth = position141, tokenIndex141, depth141
			return false
		},
		/* 27 string <- <(STRING_DELIM (!(STRING_DELIM / newline) .)* STRING_DELIM space*)> */
		func() bool {
			position147, tokenIndex147, depth147 := position, tokenIndex, depth
			{
				position148 := position
				depth++
				if !_rules[ruleSTRING_DELIM]() {
					goto l147
				}
			l149:
				{
					position150, tokenIndex150, depth150 := position, tokenIndex, depth
					{
						position151, tokenIndex151, depth151 := position, tokenIndex, depth
						{
							position152, tokenIndex152, depth152 := position, tokenIndex, depth
							if !_rules[ruleSTRING_DELIM]() {
								goto l153
							}
							goto l152
						l153:
							position, tokenIndex, depth = position152, tokenIndex152, depth152
							if !_rules[rulenewline]() {
								goto l151
							}
						}
					l152:
						goto l150
					l151:
						position, tokenIndex, depth = position151, tokenIndex151, depth151
					}
					if !matchDot() {
						goto l150
					}
					goto l149
				l150:
					position, tokenIndex, depth = position150, tokenIndex150, depth150
				}
				if !_rules[ruleSTRING_DELIM]() {
					goto l147
				}
			l154:
				{
					position155, tokenIndex155, depth155 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l155
					}
					goto l154
				l155:
					position, tokenIndex, depth = position155, tokenIndex155, depth155
				}
				depth--
				add(rulestring, position148)
			}
			return true
		l147:
			position, tokenIndex, depth = position147, tokenIndex147, depth147
			return false
		},
		/* 28 block <- <(simple_block / compound_block)> */
		func() bool {
			position156, tokenIndex156, depth156 := position, tokenIndex, depth
			{
				position157 := position
				depth++
				{
					position158, tokenIndex158, depth158 := position, tokenIndex, depth
					if !_rules[rulesimple_block]() {
						goto l159
					}
					goto l158
				l159:
					position, tokenIndex, depth = position158, tokenIndex158, depth158
					if !_rules[rulecompound_block]() {
						goto l156
					}
				}
			l158:
				depth--
				add(ruleblock, position157)
			}
			return true
		l156:
			position, tokenIndex, depth = position156, tokenIndex156, depth156
			return false
		},
		/* 29 simple_block <- <(ident block_description BLOCK_OPEN pair+ BLOCK_CLOSE)> */
		func() bool {
			position160, tokenIndex160, depth160 := position, tokenIndex, depth
			{
				position161 := position
				depth++
				if !_rules[ruleident]() {
					goto l160
				}
				if !_rules[ruleblock_description]() {
					goto l160
				}
				if !_rules[ruleBLOCK_OPEN]() {
					goto l160
				}
				if !_rules[rulepair]() {
					goto l160
				}
			l162:
				{
					position163, tokenIndex163, depth163 := position, tokenIndex, depth
					if !_rules[rulepair]() {
						goto l163
					}
					goto l162
				l163:
					position, tokenIndex, depth = position163, tokenIndex163, depth163
				}
				if !_rules[ruleBLOCK_CLOSE]() {
					goto l160
				}
				depth--
				add(rulesimple_block, position161)
			}
			return true
		l160:
			position, tokenIndex, depth = position160, tokenIndex160, depth160
			return false
		},
		/* 30 block_description <- <(INDEX_OPEN sentence INDEX_CLOSE all_space*)> */
		func() bool {
			position164, tokenIndex164, depth164 := position, tokenIndex, depth
			{
				position165 := position
				depth++
				if !_rules[ruleINDEX_OPEN]() {
					goto l164
				}
				if !_rules[rulesentence]() {
					goto l164
				}
				if !_rules[ruleINDEX_CLOSE]() {
					goto l164
				}
			l166:
				{
					position167, tokenIndex167, depth167 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l167
					}
					goto l166
				l167:
					position, tokenIndex, depth = position167, tokenIndex167, depth167
				}
				depth--
				add(ruleblock_description, position165)
			}
			return true
		l164:
			position, tokenIndex, depth = position164, tokenIndex164, depth164
			return false
		},
		/* 31 compound_block <- <(BLOCK BLOCK_OPEN pair* simple_block+ BLOCK_CLOSE rescue_clause? always_clause?)> */
		func() bool {
			position168, tokenIndex168, depth168 := position, tokenIndex, depth
			{
				position169 := position
				depth++
				if !_rules[ruleBLOCK]() {
					goto l168
				}
				if !_rules[ruleBLOCK_OPEN]() {
					goto l168
				}
			l170:
				{
					position171, tokenIndex171, depth171 := position, tokenIndex, depth
					if !_rules[rulepair]() {
						goto l171
					}
					goto l170
				l171:
					position, tokenIndex, depth = position171, tokenIndex171, depth171
				}
				if !_rules[rulesimple_block]() {
					goto l168
				}
			l172:
				{
					position173, tokenIndex173, depth173 := position, tokenIndex, depth
					if !_rules[rulesimple_block]() {
						goto l173
					}
					goto l172
				l173:
					position, tokenIndex, depth = position173, tokenIndex173, depth173
				}
				if !_rules[ruleBLOCK_CLOSE]() {
					goto l168
				}
				{
					position174, tokenIndex174, depth174 := position, tokenIndex, depth
					if !_rules[rulerescue_clause]() {
						goto l174
					}
					goto l175
				l174:
					position, tokenIndex, depth = position174, tokenIndex174, depth174
				}
			l175:
				{
					position176, tokenIndex176, depth176 := position, tokenIndex, depth
					if !_rules[rulealways_clause]() {
						goto l176
					}
					goto l177
				l176:
					position, tokenIndex, depth = position176, tokenIndex176, depth176
				}
			l177:
				depth--
				add(rulecompound_block, position169)
			}
			return true
		l168:
			position, tokenIndex, depth = position168, tokenIndex168, depth168
			return false
		},
		/* 32 rescue_clause <- <(RESCUE BLOCK_OPEN simple_block+ BLOCK_CLOSE)> */
		func() bool {
			position178, tokenIndex178, depth178 := position, tokenIndex, depth
			{
				position179 := position
				depth++
				if !_rules[ruleRESCUE]() {
					goto l178
				}
				if !_rules[ruleBLOCK_OPEN]() {
					goto l178
				}
				if !_rules[rulesimple_block]() {
					goto l178
				}
			l180:
				{
					position181, tokenIndex181, depth181 := position, tokenIndex, depth
					if !_rules[rulesimple_block]() {
						goto l181
					}
					goto l180
				l181:
					position, tokenIndex, depth = position181, tokenIndex181, depth181
				}
				if !_rules[ruleBLOCK_CLOSE]() {
					goto l178
				}
				depth--
				add(rulerescue_clause, position179)
			}
			return true
		l178:
			position, tokenIndex, depth = position178, tokenIndex178, depth178
			return false
		},
		/* 33 always_clause <- <(ALWAYS BLOCK_OPEN simple_block+ BLOCK_CLOSE)> */
		func() bool {
			position182, tokenIndex182, depth182 := position, tokenIndex, depth
			{
				position183 := position
				depth++
				if !_rules[ruleALWAYS]() {
					goto l182
				}
				if !_rules[ruleBLOCK_OPEN]() {
					goto l182
				}
				if !_rules[rulesimple_block]() {
					goto l182
				}
			l184:
				{
					position185, tokenIndex185, depth185 := position, tokenIndex, depth
					if !_rules[rulesimple_block]() {
						goto l185
					}
					goto l184
				l185:
					position, tokenIndex, depth = position185, tokenIndex185, depth185
				}
				if !_rules[ruleBLOCK_CLOSE]() {
					goto l182
				}
				depth--
				add(rulealways_clause, position183)
			}
			return true
		l182:
			position, tokenIndex, depth = position182, tokenIndex182, depth182
			return false
		},
		/* 34 end <- <!.> */
		func() bool {
			position186, tokenIndex186, depth186 := position, tokenIndex, depth
			{
				position187 := position
				depth++
				{
					position188, tokenIndex188, depth188 := position, tokenIndex, depth
					if !matchDot() {
						goto l188
					}
					goto l186
				l188:
					position, tokenIndex, depth = position188, tokenIndex188, depth188
				}
				depth--
				add(ruleend, position187)
			}
			return true
		l186:
			position, tokenIndex, depth = position186, tokenIndex186, depth186
			return false
		},
		/* 35 PAIR_DELIM <- <(':' space*)> */
		func() bool {
			position189, tokenIndex189, depth189 := position, tokenIndex, depth
			{
				position190 := position
				depth++
				if buffer[position] != rune(':') {
					goto l189
				}
				position++
			l191:
				{
					position192, tokenIndex192, depth192 := position, tokenIndex, depth
					if !_rules[rulespace]() {
						goto l192
					}
					goto l191
				l192:
					position, tokenIndex, depth = position192, tokenIndex192, depth192
				}
				depth--
				add(rulePAIR_DELIM, position190)
			}
			return true
		l189:
			position, tokenIndex, depth = position189, tokenIndex189, depth189
			return false
		},
		/* 36 DOT <- <'.'> */
		func() bool {
			position193, tokenIndex193, depth193 := position, tokenIndex, depth
			{
				position194 := position
				depth++
				if buffer[position] != rune('.') {
					goto l193
				}
				position++
				depth--
				add(ruleDOT, position194)
			}
			return true
		l193:
			position, tokenIndex, depth = position193, tokenIndex193, depth193
			return false
		},
		/* 37 STRING_DELIM <- <'\''> */
		func() bool {
			position195, tokenIndex195, depth195 := position, tokenIndex, depth
			{
				position196 := position
				depth++
				if buffer[position] != rune('\'') {
					goto l195
				}
				position++
				depth--
				add(ruleSTRING_DELIM, position196)
			}
			return true
		l195:
			position, tokenIndex, depth = position195, tokenIndex195, depth195
			return false
		},
		/* 38 COMMENT_START <- <'#'> */
		func() bool {
			position197, tokenIndex197, depth197 := position, tokenIndex, depth
			{
				position198 := position
				depth++
				if buffer[position] != rune('#') {
					goto l197
				}
				position++
				depth--
				add(ruleCOMMENT_START, position198)
			}
			return true
		l197:
			position, tokenIndex, depth = position197, tokenIndex197, depth197
			return false
		},
		/* 39 MINUS <- <'-'> */
		func() bool {
			position199, tokenIndex199, depth199 := position, tokenIndex, depth
			{
				position200 := position
				depth++
				if buffer[position] != rune('-') {
					goto l199
				}
				position++
				depth--
				add(ruleMINUS, position200)
			}
			return true
		l199:
			position, tokenIndex, depth = position199, tokenIndex199, depth199
			return false
		},
		/* 40 INDEX_OPEN <- <'['> */
		func() bool {
			position201, tokenIndex201, depth201 := position, tokenIndex, depth
			{
				position202 := position
				depth++
				if buffer[position] != rune('[') {
					goto l201
				}
				position++
				depth--
				add(ruleINDEX_OPEN, position202)
			}
			return true
		l201:
			position, tokenIndex, depth = position201, tokenIndex201, depth201
			return false
		},
		/* 41 INDEX_CLOSE <- <']'> */
		func() bool {
			position203, tokenIndex203, depth203 := position, tokenIndex, depth
			{
				position204 := position
				depth++
				if buffer[position] != rune(']') {
					goto l203
				}
				position++
				depth--
				add(ruleINDEX_CLOSE, position204)
			}
			return true
		l203:
			position, tokenIndex, depth = position203, tokenIndex203, depth203
			return false
		},
		/* 42 ARRAY_OPEN <- <('[' all_space*)> */
		func() bool {
			position205, tokenIndex205, depth205 := position, tokenIndex, depth
			{
				position206 := position
				depth++
				if buffer[position] != rune('[') {
					goto l205
				}
				position++
			l207:
				{
					position208, tokenIndex208, depth208 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l208
					}
					goto l207
				l208:
					position, tokenIndex, depth = position208, tokenIndex208, depth208
				}
				depth--
				add(ruleARRAY_OPEN, position206)
			}
			return true
		l205:
			position, tokenIndex, depth = position205, tokenIndex205, depth205
			return false
		},
		/* 43 ARRAY_CLOSE <- <(']' all_space*)> */
		func() bool {
			position209, tokenIndex209, depth209 := position, tokenIndex, depth
			{
				position210 := position
				depth++
				if buffer[position] != rune(']') {
					goto l209
				}
				position++
			l211:
				{
					position212, tokenIndex212, depth212 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l212
					}
					goto l211
				l212:
					position, tokenIndex, depth = position212, tokenIndex212, depth212
				}
				depth--
				add(ruleARRAY_CLOSE, position210)
			}
			return true
		l209:
			position, tokenIndex, depth = position209, tokenIndex209, depth209
			return false
		},
		/* 44 GROUP_OPEN <- <('(' all_space*)> */
		func() bool {
			position213, tokenIndex213, depth213 := position, tokenIndex, depth
			{
				position214 := position
				depth++
				if buffer[position] != rune('(') {
					goto l213
				}
				position++
			l215:
				{
					position216, tokenIndex216, depth216 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l216
					}
					goto l215
				l216:
					position, tokenIndex, depth = position216, tokenIndex216, depth216
				}
				depth--
				add(ruleGROUP_OPEN, position214)
			}
			return true
		l213:
			position, tokenIndex, depth = position213, tokenIndex213, depth213
			return false
		},
		/* 45 GROUP_CLOSE <- <(')' all_space*)> */
		func() bool {
			position217, tokenIndex217, depth217 := position, tokenIndex, depth
			{
				position218 := position
				depth++
				if buffer[position] != rune(')') {
					goto l217
				}
				position++
			l219:
				{
					position220, tokenIndex220, depth220 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l220
					}
					goto l219
				l220:
					position, tokenIndex, depth = position220, tokenIndex220, depth220
				}
				depth--
				add(ruleGROUP_CLOSE, position218)
			}
			return true
		l217:
			position, tokenIndex, depth = position217, tokenIndex217, depth217
			return false
		},
		/* 46 BLOCK_OPEN <- <('{' all_space*)> */
		func() bool {
			position221, tokenIndex221, depth221 := position, tokenIndex, depth
			{
				position222 := position
				depth++
				if buffer[position] != rune('{') {
					goto l221
				}
				position++
			l223:
				{
					position224, tokenIndex224, depth224 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l224
					}
					goto l223
				l224:
					position, tokenIndex, depth = position224, tokenIndex224, depth224
				}
				depth--
				add(ruleBLOCK_OPEN, position222)
			}
			return true
		l221:
			position, tokenIndex, depth = position221, tokenIndex221, depth221
			return false
		},
		/* 47 BLOCK_CLOSE <- <('}' all_space*)> */
		func() bool {
			position225, tokenIndex225, depth225 := position, tokenIndex, depth
			{
				position226 := position
				depth++
				if buffer[position] != rune('}') {
					goto l225
				}
				position++
			l227:
				{
					position228, tokenIndex228, depth228 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l228
					}
					goto l227
				l228:
					position, tokenIndex, depth = position228, tokenIndex228, depth228
				}
				depth--
				add(ruleBLOCK_CLOSE, position226)
			}
			return true
		l225:
			position, tokenIndex, depth = position225, tokenIndex225, depth225
			return false
		},
		/* 48 BLOCK <- <('b' 'l' 'o' 'c' 'k' !alpha all_space*)> */
		func() bool {
			position229, tokenIndex229, depth229 := position, tokenIndex, depth
			{
				position230 := position
				depth++
				if buffer[position] != rune('b') {
					goto l229
				}
				position++
				if buffer[position] != rune('l') {
					goto l229
				}
				position++
				if buffer[position] != rune('o') {
					goto l229
				}
				position++
				if buffer[position] != rune('c') {
					goto l229
				}
				position++
				if buffer[position] != rune('k') {
					goto l229
				}
				position++
				{
					position231, tokenIndex231, depth231 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l231
					}
					goto l229
				l231:
					position, tokenIndex, depth = position231, tokenIndex231, depth231
				}
			l232:
				{
					position233, tokenIndex233, depth233 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l233
					}
					goto l232
				l233:
					position, tokenIndex, depth = position233, tokenIndex233, depth233
				}
				depth--
				add(ruleBLOCK, position230)
			}
			return true
		l229:
			position, tokenIndex, depth = position229, tokenIndex229, depth229
			return false
		},
		/* 49 RESCUE <- <('r' 'e' 's' 'c' 'u' 'e' !alpha all_space*)> */
		func() bool {
			position234, tokenIndex234, depth234 := position, tokenIndex, depth
			{
				position235 := position
				depth++
				if buffer[position] != rune('r') {
					goto l234
				}
				position++
				if buffer[position] != rune('e') {
					goto l234
				}
				position++
				if buffer[position] != rune('s') {
					goto l234
				}
				position++
				if buffer[position] != rune('c') {
					goto l234
				}
				position++
				if buffer[position] != rune('u') {
					goto l234
				}
				position++
				if buffer[position] != rune('e') {
					goto l234
				}
				position++
				{
					position236, tokenIndex236, depth236 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l236
					}
					goto l234
				l236:
					position, tokenIndex, depth = position236, tokenIndex236, depth236
				}
			l237:
				{
					position238, tokenIndex238, depth238 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l238
					}
					goto l237
				l238:
					position, tokenIndex, depth = position238, tokenIndex238, depth238
				}
				depth--
				add(ruleRESCUE, position235)
			}
			return true
		l234:
			position, tokenIndex, depth = position234, tokenIndex234, depth234
			return false
		},
		/* 50 ALWAYS <- <('a' 'l' 'w' 'a' 'y' 's' !alpha all_space*)> */
		func() bool {
			position239, tokenIndex239, depth239 := position, tokenIndex, depth
			{
				position240 := position
				depth++
				if buffer[position] != rune('a') {
					goto l239
				}
				position++
				if buffer[position] != rune('l') {
					goto l239
				}
				position++
				if buffer[position] != rune('w') {
					goto l239
				}
				position++
				if buffer[position] != rune('a') {
					goto l239
				}
				position++
				if buffer[position] != rune('y') {
					goto l239
				}
				position++
				if buffer[position] != rune('s') {
					goto l239
				}
				position++
				{
					position241, tokenIndex241, depth241 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l241
					}
					goto l239
				l241:
					position, tokenIndex, depth = position241, tokenIndex241, depth241
				}
			l242:
				{
					position243, tokenIndex243, depth243 := position, tokenIndex, depth
					if !_rules[ruleall_space]() {
						goto l243
					}
					goto l242
				l243:
					position, tokenIndex, depth = position243, tokenIndex243, depth243
				}
				depth--
				add(ruleALWAYS, position240)
			}
			return true
		l239:
			position, tokenIndex, depth = position239, tokenIndex239, depth239
			return false
		},
		/* 51 TRUE <- <('t' 'r' 'u' 'e' !alpha)> */
		func() bool {
			position244, tokenIndex244, depth244 := position, tokenIndex, depth
			{
				position245 := position
				depth++
				if buffer[position] != rune('t') {
					goto l244
				}
				position++
				if buffer[position] != rune('r') {
					goto l244
				}
				position++
				if buffer[position] != rune('u') {
					goto l244
				}
				position++
				if buffer[position] != rune('e') {
					goto l244
				}
				position++
				{
					position246, tokenIndex246, depth246 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l246
					}
					goto l244
				l246:
					position, tokenIndex, depth = position246, tokenIndex246, depth246
				}
				depth--
				add(ruleTRUE, position245)
			}
			return true
		l244:
			position, tokenIndex, depth = position244, tokenIndex244, depth244
			return false
		},
		/* 52 FALSE <- <('f' 'a' 'l' 's' 'e' !alpha)> */
		func() bool {
			position247, tokenIndex247, depth247 := position, tokenIndex, depth
			{
				position248 := position
				depth++
				if buffer[position] != rune('f') {
					goto l247
				}
				position++
				if buffer[position] != rune('a') {
					goto l247
				}
				position++
				if buffer[position] != rune('l') {
					goto l247
				}
				position++
				if buffer[position] != rune('s') {
					goto l247
				}
				position++
				if buffer[position] != rune('e') {
					goto l247
				}
				position++
				{
					position249, tokenIndex249, depth249 := position, tokenIndex, depth
					if !_rules[rulealpha]() {
						goto l249
					}
					goto l247
				l249:
					position, tokenIndex, depth = position249, tokenIndex249, depth249
				}
				depth--
				add(ruleFALSE, position248)
			}
			return true
		l247:
			position, tokenIndex, depth = position247, tokenIndex247, depth247
			return false
		},
		/* 53 EQ <- <('=' '=' all_space*)> */
		nil,
		/* 54 LT <- <('<' all_space*)> */
		nil,
		/* 55 GT <- <('>' all_space*)> */
		nil,
		/* 56 LTE <- <('<' '=' all_space*)> */
		nil,
		/* 57 GTE <- <('>' '=' all_space*)> */
		nil,
		/* 58 AND <- <('&' '&' all_space*)> */
		nil,
		/* 59 OR <- <('|' '|' all_space*)> */
		nil,
	}
	p.rules = _rules
}
