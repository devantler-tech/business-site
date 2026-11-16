package identity

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// orderAddon is one add-on of an order: the catalogue's id and kind, the units
// and the VAT-inclusive unit price when the order was placed.
type orderAddon struct {
	ID, Kind  string
	Quantity  int
	UnitMinor int64
	LastMonth *time.Time // the last billed month, once the client ended a monthly add-on
	Removed   *time.Time // when Stripe confirmed a card subscription no longer charges it
}

func (a orderAddon) TotalMinor() int64 { return a.UnitMinor * int64(a.Quantity) }
func (a orderAddon) TotalDKK() int64   { return a.TotalMinor() / 100 }
func (a orderAddon) Monthly() bool     { return a.Kind == "monthly" }

// Title is the add-on's published title in lang, with its units when more than
// one, or its id when the catalogue no longer lists it.
func (a orderAddon) Title(lang string) string {
	title := a.ID
	if offer, ok := findAddon(a.ID); ok {
		title = offer.text(lang).Title
	}
	if a.Quantity > 1 {
		title += " × " + strconv.Itoa(a.Quantity)
	}
	return title
}

// Value is the order form's encoding of the add-on: its id, units and the
// unit price in kroner the client was shown.
func (a orderAddon) Value() string {
	return a.ID + ":" + strconv.Itoa(a.Quantity) + ":" + strconv.FormatInt(a.UnitMinor/100, 10)
}

func (a addonOffer) text(lang string) addonText {
	if lang == "da" {
		return a.Text["da"]
	}
	return a.Text["en"]
}

func (a addonOffer) units() int { return max(a.Max, 1) }

// addonChoice is a published add-on as the order page offers it.
type addonChoice struct {
	addonOffer
	Selected bool
	Quantity int
}

func (c addonChoice) Title(lang string) string       { return c.text(lang).Title }
func (c addonChoice) Description(lang string) string { return c.text(lang).Description }
func (c addonChoice) Monthly() bool                  { return c.Kind == "monthly" }

// Units are the quantities the form offers, from 1 to the add-on's limit.
func (c addonChoice) Units() []int {
	units := make([]int, c.units())
	for i := range units {
		units[i] = i + 1
	}
	return units
}

func findAddon(id string) (addonOffer, bool) {
	for _, a := range offerings.Addons {
		if a.ID == id {
			return a, true
		}
	}
	return addonOffer{}, false
}

// availableAddons are the published add-ons for an offering, in catalogue
// order; monthly ones only for an order with hosting.
func availableAddons(product string, hosting bool) []addonOffer {
	var result []addonOffer
	for _, a := range offerings.Addons {
		if slices.Contains(a.Offerings, product) && (hosting || a.Kind != "monthly") {
			result = append(result, a)
		}
	}
	return result
}

// parseAddons reads the order form's "id:units:price" values, in catalogue
// order. It refuses an unknown, unavailable or repeated add-on, units outside
// the add-on's limit, a second priority tier and a price the catalogue no
// longer has, so an order never costs more than the client was shown.
func parseAddons(values []string, product string, hosting bool) ([]orderAddon, bool) {
	chosen, shown := map[string]int{}, map[string]int64{}
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) != 3 {
			return nil, false
		}
		n, err := strconv.Atoi(parts[1])
		price, priceErr := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || priceErr != nil || chosen[parts[0]] != 0 {
			return nil, false
		}
		chosen[parts[0]], shown[parts[0]] = n, price
	}
	var result []orderAddon
	tiers := 0
	for _, a := range availableAddons(product, hosting) {
		n, ok := chosen[a.ID]
		if !ok {
			continue
		}
		delete(chosen, a.ID)
		if n < 1 || n > a.units() || shown[a.ID] != a.Price {
			return nil, false
		}
		if a.Tier != "" {
			tiers++
		}
		result = append(result, orderAddon{ID: a.ID, Kind: a.Kind, Quantity: n, UnitMinor: a.Price * 100})
	}
	if len(chosen) != 0 || tiers > 1 {
		return nil, false
	}
	return result, true
}

// selectedAddons reads the add-ons the client ticked on the receipt page: an
// "addon" value per add-on and an optional "units-<id>" for its quantity.
func selectedAddons(query url.Values, product string, hosting bool) ([]addonChoice, []orderAddon, bool) {
	values := make([]string, 0, len(query["addon"]))
	for _, id := range query["addon"] {
		units := query.Get("units-" + id)
		if units == "" {
			units = "1"
		}
		price := "0"
		if a, ok := findAddon(id); ok {
			price = strconv.FormatInt(a.Price, 10)
		}
		values = append(values, id+":"+units+":"+price)
	}
	chosen, valid := parseAddons(values, product, hosting)
	if !valid {
		chosen = nil
	}
	// An invalid choice stays ticked, so the client can correct it.
	var choices []addonChoice
	for _, a := range availableAddons(product, hosting) {
		c := addonChoice{addonOffer: a, Quantity: 1, Selected: slices.Contains(query["addon"], a.ID)}
		if n, err := strconv.Atoi(query.Get("units-" + a.ID)); err == nil && n >= 1 && n <= a.units() {
			c.Quantity = n
		}
		choices = append(choices, c)
	}
	return choices, chosen, valid
}

// sameAddons reports whether the form's values name exactly an order's stored
// add-ons, at their stored prices.
func sameAddons(stored []orderAddon, values []string) bool {
	named := make([]string, len(stored))
	for i, a := range stored {
		named[i] = a.Value()
	}
	sorted := slices.Clone(values)
	slices.Sort(named)
	slices.Sort(sorted)
	return slices.Equal(named, sorted)
}

// addonTotals sums an order's one-time and monthly add-ons.
func addonTotals(addons []orderAddon) (oneTime, monthly int64) {
	for _, a := range addons {
		if a.Monthly() {
			monthly += a.TotalMinor()
		} else {
			oneTime += a.TotalMinor()
		}
	}
	return oneTime, monthly
}

// readOrderAddons returns an order's add-ons in catalogue order.
func readOrderAddons(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, order string) ([]orderAddon, error) {
	rows, err := q.Query(ctx, `SELECT a.addon_id,a.kind,a.quantity,a.unit_minor,e.last_month,r.removed_at FROM portal_order_addons a LEFT JOIN portal_addon_ends e ON e.order_id=a.order_id AND e.addon_id=a.addon_id
 LEFT JOIN portal_addon_removals r ON r.order_id=a.order_id AND r.addon_id=a.addon_id WHERE a.order_id=$1`, order)
	if err != nil {
		return nil, errTriageStorage
	}
	addons, err := pgx.CollectRows(rows, pgx.RowToStructByPos[orderAddon])
	if err != nil {
		return nil, errTriageStorage
	}
	slices.SortFunc(addons, func(a, b orderAddon) int { return addonRank(a.ID) - addonRank(b.ID) })
	return addons, nil
}

func addonRank(id string) int {
	for i, a := range offerings.Addons {
		if a.ID == id {
			return i
		}
	}
	return len(offerings.Addons)
}

// periodAddons are the add-ons a billing period charges: the one-time ones
// with the order's first charge, the monthly ones every month until the client
// ends them. A period that names no month (a new subscription) charges no
// ended add-on.
func periodAddons(addons []orderAddon, period string) []orderAddon {
	var result []orderAddon
	for _, a := range addons {
		if a.Monthly() == (period != "acceptance") && !endedBefore(a, period) {
			result = append(result, a)
		}
	}
	return result
}

// endedBefore reports whether a monthly add-on ended before the period.
func endedBefore(a orderAddon, period string) bool {
	if !a.Monthly() || a.LastMonth == nil {
		return false
	}
	month, err := time.Parse("2006-01", period)
	return err != nil || a.LastMonth.Before(month)
}

// periodMonthly is what a monthly period charges: the order's monthly price
// less the add-ons the client ended before it.
func periodMonthly(monthly int64, addons []orderAddon, period string) int64 {
	for _, a := range addons {
		if endedBefore(a, period) {
			monthly -= a.TotalMinor()
		}
	}
	return monthly
}

// addonLine names an add-on on an invoice or payment line.
func addonLine(a orderAddon, lang string) string {
	if a.Monthly() {
		return fmt.Sprintf("%s (%s)", a.Title(lang), map[bool]string{true: "månedligt", false: "monthly"}[lang == "da"])
	}
	return a.Title(lang)
}
