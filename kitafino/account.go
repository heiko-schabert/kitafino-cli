package kitafino

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Account is the child the login belongs to; kitafino has one login per child.
type Account struct {
	Name       string  `json:"name"`
	CustomerID string  `json:"customer_id"`
	Balance    float64 `json:"balance_eur"`
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	b, err := c.fetch(ctx, "?action=bestellen")
	if err != nil {
		return Account{}, err
	}
	return parseAccount(b)
}

func parseAccount(b []byte) (Account, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
	if err != nil {
		return Account{}, err
	}
	user := doc.Find(`#joy2 a`).First()
	if user.Length() == 0 {
		return Account{}, errors.New("account name missing, kitafino layout changed?")
	}
	a := Account{
		Name:       ownText(user),
		CustomerID: strings.Trim(collapse(user.Find("#joy2_1").Text()), "[]"),
	}
	a.Balance, err = balance(doc)
	return a, err
}
