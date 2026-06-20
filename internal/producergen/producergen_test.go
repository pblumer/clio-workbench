package producergen

import (
	"go/format"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

func orderModel() model.Draft {
	return model.Draft{
		Name:         "Order",
		SubjectStyle: "/orders/{id}",
		Steps: []model.Step{
			{Kind: model.StepEvent, Name: "order-paid", Fields: []model.Field{
				{Name: "id", Type: "reference", Format: "uuid", Required: true},
				{Name: "amount", Type: "number", Required: true},
				{Name: "qty", Type: "integer"},
				{Name: "gift", Type: "boolean"},
				{Name: "currency", Type: "enum", Enum: []string{"EUR", "USD"}},
				{Name: "", Type: "string"}, // unnamed → skipped
			}},
			{Kind: model.StepTask, Name: "ship"}, // task → skipped
			{Kind: model.StepEvent, Name: "  "},  // blank → skipped
		},
	}
}

func TestLanguages(t *testing.T) {
	langs := Languages()
	if len(langs) != 4 {
		t.Fatalf("want 4 languages, got %d", len(langs))
	}
	if !SupportedLang("go") || !SupportedLang("curl") {
		t.Errorf("go/curl should be supported")
	}
	if SupportedLang("cobol") {
		t.Errorf("cobol should not be supported")
	}
}

func TestGenerateUnknownLang(t *testing.T) {
	if _, err := Generate(orderModel(), "cobol"); err == nil {
		t.Fatal("expected error for unknown language")
	}
}

func TestGenerateGoIsValid(t *testing.T) {
	src, err := Generate(orderModel(), "go")
	if err != nil {
		t.Fatalf("generate go: %v", err)
	}
	// The output must already be gofmt'd valid Go (format is idempotent).
	reformatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("generated go does not parse: %v", err)
	}
	if string(reformatted) != src {
		t.Errorf("generated go is not gofmt-stable")
	}
	for _, want := range []string{
		"package producer",
		"type OrderPaidData struct {",
		`json:"id"`,
		`json:"amount"`,
		`json:"currency"`,
		"float64", // number → float64
		"bool",    // boolean → bool
		"func (c *Client) SendOrderPaid(subject string, data OrderPaidData) error",
		`c.send("order-paid", subject, data)`,
		eventsPath,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("go output missing %q", want)
		}
	}
	// Task and blank steps must not appear.
	if strings.Contains(src, "ship") {
		t.Errorf("task step leaked into producer code")
	}
}

func TestGenerateTypeScript(t *testing.T) {
	src, err := Generate(orderModel(), "ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export interface OrderPaidData {",
		`"id": string;`,
		`"amount": number;`,
		`"gift": boolean;`,
		"crypto.randomUUID()",
		"export function sendOrderPaid(subject: string, data: OrderPaidData)",
		eventsPath,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("ts output missing %q", want)
		}
	}
}

func TestGeneratePython(t *testing.T) {
	src, err := Generate(orderModel(), "python")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"@dataclass",
		"class OrderPaid:",
		"amount: float",
		"qty: int",
		"def send_order_paid(subject: str, data: OrderPaid) -> None:",
		`send("order-paid", subject, {"id": data.id`,
		"uuid.uuid4()",
		eventsPath,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("python output missing %q", want)
		}
	}
}

func TestGenerateCurl(t *testing.T) {
	src, err := Generate(orderModel(), "curl")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		`send "order-paid" "$SUBJECT"`,
		`"id":""`,
		`"amount":0`,
		`"gift":false`,
		`"currency":"EUR"`, // enum → first value
		eventsPath,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("curl output missing %q", want)
		}
	}
}

func TestGenerateEmptyModel(t *testing.T) {
	// No event types: every language still emits a usable client/skeleton.
	d := model.Draft{Name: "Empty"}
	goSrc, err := Generate(d, "go")
	if err != nil {
		t.Fatalf("go empty: %v", err)
	}
	if !strings.Contains(goSrc, "func NewClient()") {
		t.Errorf("empty go missing client")
	}
	py, _ := Generate(d, "python")
	if !strings.Contains(py, "def send(") {
		t.Errorf("empty python missing send()")
	}
}

func TestGenerateEdgeCases(t *testing.T) {
	d := model.Draft{Name: "X", Steps: []model.Step{
		{Kind: model.StepEvent, Name: "ping"},                                                   // no fields
		{Kind: model.StepEvent, Name: "pick", Fields: []model.Field{{Name: "k", Type: "enum"}}}, // empty enum
	}}
	py, _ := Generate(d, "python")
	if !strings.Contains(py, "class Ping:") || !strings.Contains(py, "    pass") {
		t.Errorf("empty dataclass should use 'pass':\n%s", py)
	}
	sh, _ := Generate(d, "curl")
	if !strings.Contains(sh, `"k":""`) {
		t.Errorf("empty enum should yield empty-string sample:\n%s", sh)
	}
	if upFirst("") != "" {
		t.Errorf("upFirst('') should be empty")
	}
}

func TestIdentifierHelpers(t *testing.T) {
	cases := []struct{ in, pascal, snake string }{
		{"order-paid", "OrderPaid", "order_paid"},
		{"order.shipped", "OrderShipped", "order_shipped"},
		{"OrderPaid", "OrderPaid", "orderpaid"},
		{"", "Event", "event"},        // empty → fallback
		{"123go", "X123go", "x123go"}, // digit-leading → prefixed
	}
	for _, c := range cases {
		if got := pascal(c.in); got != c.pascal {
			t.Errorf("pascal(%q) = %q, want %q", c.in, got, c.pascal)
		}
		if got := snake(c.in); got != c.snake {
			t.Errorf("snake(%q) = %q, want %q", c.in, got, c.snake)
		}
	}
}
