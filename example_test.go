package structmap_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MisterLaker/structmap"
)

func ExampleMarshal() {
	type Address struct {
		City string `map:"city"`
		Zip  string `map:"zip,private"`
	}

	type User struct {
		ID        string    `map:"id"`
		Email     string    `map:"email,private"`
		Address   Address   `map:"address"`
		CreatedAt time.Time `map:"created_at,omitzero"`
	}

	user := User{ID: "42", Email: "a@example.com", Address: Address{City: "Kyiv", Zip: "01001"}}

	public, _ := structmap.Marshal("map", "public", user)
	full, _ := structmap.Marshal("map", "", user)

	fmt.Println(public)
	fmt.Println(full)
	// Output:
	// map[address:map[city:Kyiv] id:42]
	// map[address:map[city:Kyiv zip:01001] email:a@example.com id:42]
}

func ExampleUnmarshal() {
	var m map[string]any
	_ = json.Unmarshal([]byte(`{"id": "42", "age": 30, "created_at": "2026-10-29T12:00:00Z"}`), &m)

	var user struct {
		ID        string    `map:"id"`
		Age       int       `map:"age"`
		CreatedAt time.Time `map:"created_at"`
	}

	err := structmap.Unmarshal("map", "", m, &user)

	fmt.Println(user.ID, user.Age, user.CreatedAt.Format(time.DateOnly), err)
	// Output: 42 30 2026-10-29 <nil>
}

func ExampleCodec() {
	type Item struct {
		SKU   string `db:"sku"`
		Price int    `db:"price,omitempty"`
	}

	codec := structmap.Codec{Tag: "db"}

	m, _ := codec.Marshal("", Item{SKU: "a1"})
	fmt.Println(m)

	var item Item
	_ = codec.Unmarshal("", map[string]any{"sku": "b2", "price": 5}, &item)
	fmt.Println(item)
	// Output:
	// map[sku:a1]
	// {b2 5}
}

// Money is stored as a decimal string.
type Money int64

func (m Money) MarshalMap(structmap.Scope) (any, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *Money) UnmarshalMap(_ structmap.Scope, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("money: unexpected %T", v)
	}

	var units, cents int64
	if _, err := fmt.Sscanf(s, "%d.%d", &units, &cents); err != nil {
		return fmt.Errorf("money: %w", err)
	}

	*m = Money(units*100 + cents)

	return nil
}

func ExampleMarshaler() {
	type Order struct {
		Total Money `map:"total"`
	}

	m, _ := structmap.Marshal("map", "", Order{Total: 1250})
	fmt.Println(m)

	var order Order
	err := structmap.Unmarshal("map", "", map[string]any{"total": "3.05"}, &order)
	fmt.Println(int64(order.Total), err)
	// Output:
	// map[total:12.50]
	// 305 <nil>
}

func ExampleFieldError() {
	type Item struct {
		Qty uint8 `map:"qty"`
	}

	var item Item
	err := structmap.Unmarshal("map", "", map[string]any{"qty": 300}, &item)

	var fe *structmap.FieldError
	fmt.Println(errors.As(err, &fe), fe.Path, errors.Is(err, structmap.ErrOverflow))
	fmt.Println(err)
	// Output:
	// true qty true
	// qty: overflow: cannot decode 300 into uint8
}
