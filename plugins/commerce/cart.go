package commerce

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	corecontent "github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	cartCookie = "gp_cart"

	// A per-line ceiling keeps accidental/malicious submissions bounded and
	// makes every price * quantity calculation safe to validate before writing.
	maxCartItemQty   = 999
	maxCartUnitPrice = math.MaxInt64 / int64(maxCartItemQty)
)

var (
	ErrInvalidCartQuantity     = errors.New("commerce: invalid cart quantity")
	ErrProductUnavailable      = errors.New("commerce: product is unavailable")
	ErrProductDataMissing      = errors.New("commerce: product data is missing")
	ErrInvalidProductPrice     = errors.New("commerce: invalid product price")
	ErrProductCurrencyMismatch = errors.New("commerce: product currency mismatch")
	ErrInsufficientStock       = errors.New("commerce: insufficient stock")
	ErrCartItemNotFound        = errors.New("commerce: cart item not found")
	ErrCartUnavailable         = errors.New("commerce: cart is unavailable")
	ErrCheckoutCartChanged     = errors.New("commerce: cart product changed during checkout")
)

// CartService owns cart resolution and mutation. It is stateless (built per
// request from the plugin) and uses the engine DB + repositories directly.
type CartService struct{ p *Plugin }

func (p *Plugin) cart() *CartService { return &CartService{p: p} }

// CartLine is one rendered cart line (prices recomputed live from product data).
type CartLine struct {
	ItemID    uint
	ProductID uint
	Title     string
	URL       string
	Qty       int
	UnitPrice int64
	LineTotal int64
}

func (l CartLine) UnitPriceStr() string { return formatPrice(l.UnitPrice) }
func (l CartLine) LineTotalStr() string { return formatPrice(l.LineTotal) }

// CartView is the storefront cart snapshot.
type CartView struct {
	Currency  string
	Lines     []CartLine
	Subtotal  int64
	ItemCount int
	Empty     bool
	// cartID is an internal checkout snapshot anchor. It is deliberately not
	// exposed to templates or clients: successful checkout consumes only the
	// quantities captured from this exact cart instead of deleting whatever
	// happens to be in the cart after the gateway round trip.
	cartID      uint
	checkoutKey string
}

func (v CartView) SubtotalStr() string { return formatPrice(v.Subtotal) }

// resolve returns the current cart, reconciling a guest cart into the user
// account on login. When create is false it returns nil instead of creating an
// empty cart (used by read paths like the mini-cart badge and cart view).
func (s *CartService) resolve(c *gin.Context, create bool) *Cart {
	db := s.p.engine.DB
	u := user.CurrentUser(c)
	token, _ := c.Cookie(cartCookie)

	if u != nil {
		var userCart Cart
		hasUserCart := db.First(&userCart, "user_id = ?", u.ID).Error == nil
		if token != "" {
			if guest := s.byToken(db, token); guest != nil {
				if !hasUserCart {
					// Adopt the guest cart as the user's cart.
					uid := u.ID
					guest.UserID = &uid
					guest.Token = ""
					db.Save(guest)
					s.clearCookie(c)
					return guest
				}
				if err := s.mergeItems(db, guest.ID, userCart.ID); err == nil {
					s.clearCookie(c)
				}
			}
		}
		if hasUserCart {
			return &userCart
		}
		if !create {
			return nil
		}
		uid := u.ID
		nc := &Cart{UserID: &uid, Currency: s.p.storeCurrency()}
		db.Create(nc)
		return nc
	}

	// Guest.
	if token != "" {
		if cart := s.byToken(db, token); cart != nil {
			return cart
		}
	}
	if !create {
		return nil
	}
	nc := &Cart{Token: newCartToken(), Currency: s.p.storeCurrency()}
	db.Create(nc)
	s.setCookie(c, nc.Token)
	return nc
}

func (s *CartService) byToken(db *gorm.DB, token string) *Cart {
	if token == "" {
		return nil
	}
	var cart Cart
	if err := db.First(&cart, "token = ?", token).Error; err != nil {
		return nil
	}
	return &cart
}

// mergeItems folds the guest cart's items into the destination cart, summing
// quantities for products already present. It locks both cart rows and deletes
// the source in the same transaction; upsertItem clears the destination's
// checkout key before commit, so checkout can observe neither a partially
// merged cart nor an old key bound to the new contents.
func (s *CartService) mergeItems(db *gorm.DB, fromCart, toCart uint) error {
	var items []CartItem
	if err := db.Where("cart_id = ?", fromCart).Order("id asc").Find(&items).Error; err != nil {
		return err
	}
	type mergeCandidate struct {
		item    CartItem
		product *cartProduct
	}
	candidates := make(map[uint]mergeCandidate, len(items))
	for _, it := range items {
		product, err := s.loadProduct(it.ProductContentID)
		if err != nil {
			continue
		}
		candidates[it.ID] = mergeCandidate{item: it, product: product}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		first, second := fromCart, toCart
		if first > second {
			first, second = second, first
		}
		var locked Cart
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, first).Error; err != nil {
			return err
		}
		if second != first {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, second).Error; err != nil {
				return err
			}
		}
		var current []CartItem
		if err := tx.Where("cart_id = ?", fromCart).Order("id asc").Find(&current).Error; err != nil {
			return err
		}
		if len(current) != len(items) {
			return ErrCartUnavailable
		}
		for i := range current {
			if current[i].ID != items[i].ID || current[i].ProductContentID != items[i].ProductContentID || current[i].Qty != items[i].Qty {
				return ErrCartUnavailable
			}
			candidate, ok := candidates[current[i].ID]
			if !ok {
				continue
			}
			if err := s.upsertItem(tx, toCart, candidate.item.ProductContentID, candidate.item.Qty,
				candidate.product.price, candidate.product.maxQty()); err != nil {
				return err
			}
		}
		if err := tx.Where("cart_id = ?", fromCart).Delete(&CartItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Cart{}, fromCart).Error
	})
}

// Add adds qty of a product to the current cart (creating the cart if needed).
func (s *CartService) Add(c *gin.Context, productID uint, qty int) error {
	if productID == 0 {
		return ErrProductUnavailable
	}
	if err := validateCartQuantity(qty); err != nil {
		return err
	}
	product, err := s.loadProduct(productID)
	if err != nil {
		return err
	}
	cart := s.resolve(c, true)
	if cart == nil || cart.ID == 0 {
		return ErrCartUnavailable
	}
	if !strings.EqualFold(strings.TrimSpace(cart.Currency), product.currency) {
		return ErrProductCurrencyMismatch
	}

	return s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		// Serialise mutations for a cart. This also prevents two concurrent adds
		// from both observing the same old quantity and bypassing the line cap.
		var locked Cart
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, cart.ID).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		return s.upsertItem(tx, cart.ID, productID, qty, product.price, product.maxQty())
	})
}

func (s *CartService) upsertItem(db *gorm.DB, cartID, productID uint, qty int, price int64, maxQty int) error {
	if err := validateCartQuantity(qty); err != nil {
		return err
	}
	if maxQty < 1 {
		return ErrInsufficientStock
	}
	if maxQty > maxCartItemQty {
		maxQty = maxCartItemQty
	}
	if _, err := cartLineTotal(price, qty); err != nil {
		return err
	}

	var item CartItem
	if err := db.First(&item, "cart_id = ? AND product_content_id = ?", cartID, productID).Error; err == nil {
		if item.Qty < 1 || item.Qty > maxQty || qty > maxQty-item.Qty {
			if maxQty < maxCartItemQty {
				return ErrInsufficientStock
			}
			return ErrInvalidCartQuantity
		}
		item.Qty += qty // guarded above; cannot overflow
		if _, err := cartLineTotal(price, item.Qty); err != nil {
			return err
		}
		item.UnitPriceCache = price
		if err := db.Save(&item).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		return invalidateCartCheckoutKey(db, cartID, "")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	if qty > maxQty {
		if maxQty < maxCartItemQty {
			return ErrInsufficientStock
		}
		return ErrInvalidCartQuantity
	}
	if err := db.Create(&CartItem{CartID: cartID, ProductContentID: productID, Qty: qty, UnitPriceCache: price}).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	return invalidateCartCheckoutKey(db, cartID, "")
}

// SetItemQty sets a line's positive quantity. Removal has a separate endpoint;
// ownership is enforced by querying the item together with the current cart ID.
func (s *CartService) SetItemQty(c *gin.Context, itemID uint, qty int) error {
	if itemID == 0 {
		return ErrCartItemNotFound
	}
	if err := validateCartQuantity(qty); err != nil {
		return err
	}
	cart := s.resolve(c, false)
	if cart == nil {
		return ErrCartItemNotFound
	}
	db := s.p.engine.DB
	var item CartItem
	if err := db.First(&item, "id = ? AND cart_id = ?", itemID, cart.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCartItemNotFound // same result for absent and non-owned (IDOR-safe)
		}
		return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	product, err := s.loadProduct(item.ProductContentID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(cart.Currency), product.currency) {
		return ErrProductCurrencyMismatch
	}
	if qty > product.maxQty() {
		return ErrInsufficientStock
	}
	if _, err := cartLineTotal(product.price, qty); err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var lockedCart Cart
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedCart, cart.ID).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		var locked CartItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, "id = ? AND cart_id = ?", itemID, cart.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCartItemNotFound
			}
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		locked.Qty = qty
		locked.UnitPriceCache = product.price
		if err := tx.Save(&locked).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		return invalidateCartCheckoutKey(tx, cart.ID, "")
	})
}

// RemoveItem deletes a line from the current cart (ownership enforced).
func (s *CartService) RemoveItem(c *gin.Context, itemID uint) error {
	if itemID == 0 {
		return ErrCartItemNotFound
	}
	cart := s.resolve(c, false)
	if cart == nil {
		return ErrCartItemNotFound
	}
	return s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var lockedCart Cart
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedCart, cart.ID).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		result := tx.Where("id = ? AND cart_id = ?", itemID, cart.ID).Delete(&CartItem{})
		if result.Error != nil {
			return fmt.Errorf("%w: %v", ErrCartUnavailable, result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrCartItemNotFound // does not reveal whether another cart owns it
		}
		return invalidateCartCheckoutKey(tx, cart.ID, "")
	})
}

// View returns the current cart rendered for display (empty when no cart yet).
// Prices are recomputed live from product data so a price change is reflected.
func (s *CartService) View(c *gin.Context) CartView {
	cart := s.resolve(c, false)
	cv := CartView{Currency: s.p.storeCurrency(), Empty: true}
	if cart == nil {
		return cv
	}
	cv.Currency = cart.Currency
	cv.cartID = cart.ID
	if cart.CheckoutKey != nil {
		cv.checkoutKey = *cart.CheckoutKey
	}
	var items []CartItem
	s.p.engine.DB.Where("cart_id = ?", cart.ID).Order("id asc").Find(&items)
	for _, it := range items {
		if err := validateCartQuantity(it.Qty); err != nil {
			continue
		}
		product, err := s.loadProduct(it.ProductContentID)
		if err != nil || !strings.EqualFold(strings.TrimSpace(cart.Currency), product.currency) {
			continue
		}
		line, err := cartLineTotal(product.price, it.Qty)
		if err != nil || line > math.MaxInt64-cv.Subtotal || it.Qty > math.MaxInt-cv.ItemCount {
			continue
		}
		cv.Lines = append(cv.Lines, CartLine{
			ItemID: it.ID, ProductID: it.ProductContentID, Title: product.title, URL: product.url,
			Qty: it.Qty, UnitPrice: product.price, LineTotal: line,
		})
		cv.Subtotal += line // guarded above
		cv.ItemCount += it.Qty
	}
	cv.Empty = len(cv.Lines) == 0
	return cv
}

// consumeSnapshot removes exactly the quantities that were copied into an
// order. It never resolves the cart again (which could merge a guest cart after
// login) and never performs a blanket delete after the payment gateway round
// trip. If another request added quantity meanwhile, only the ordered quantity
// is subtracted; if it removed/decreased/replaced a line, that newer state is
// preserved. Every line is consumed in one transaction so a database failure
// cannot leave a partially cleaned cart.
func (s *CartService) consumeSnapshot(snapshot CartView) error {
	if snapshot.cartID == 0 || len(snapshot.Lines) == 0 {
		return nil
	}
	if s == nil || s.p == nil || s.p.engine == nil || s.p.engine.DB == nil {
		return ErrCartUnavailable
	}
	return s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		for _, line := range snapshot.Lines {
			if line.ItemID == 0 {
				return ErrCartItemNotFound
			}
			if err := validateCartQuantity(line.Qty); err != nil {
				return err
			}

			// A larger current quantity means another request added items after
			// checkout took its snapshot. Preserve that delta atomically.
			result := tx.Model(&CartItem{}).
				Where("id = ? AND cart_id = ? AND qty > ?", line.ItemID, snapshot.cartID, line.Qty).
				UpdateColumn("qty", gorm.Expr("qty - ?", line.Qty))
			if result.Error != nil {
				return fmt.Errorf("%w: %v", ErrCartUnavailable, result.Error)
			}
			if result.RowsAffected > 0 {
				continue
			}

			// Delete only an unchanged snapshot line. A missing line or a lower
			// quantity reflects a newer user mutation and is intentionally kept.
			result = tx.Where("id = ? AND cart_id = ? AND qty = ?", line.ItemID, snapshot.cartID, line.Qty).
				Delete(&CartItem{})
			if result.Error != nil {
				return fmt.Errorf("%w: %v", ErrCartUnavailable, result.Error)
			}
		}
		return invalidateCartCheckoutKey(tx, snapshot.cartID, snapshot.checkoutKey)
	})
}

// checkoutKey returns the active 256-bit checkout key for a cart. The
// conditional UPDATE is the compare-and-set: concurrent GETs may generate
// different candidates, but exactly one installs a key and every caller reads
// that same winner. A cart mutation takes the same row lock/update path and
// clears the key, so an old form can never describe a changed cart.
func (s *CartService) checkoutKey(cartID uint) (string, error) {
	if cartID == 0 || s == nil || s.p == nil || s.p.engine == nil || s.p.engine.DB == nil {
		return "", ErrCartUnavailable
	}
	for attempt := 0; attempt < 3; attempt++ {
		candidate, err := newCheckoutKey()
		if err != nil {
			return "", err
		}
		result := s.p.engine.DB.Model(&Cart{}).
			Where("id = ? AND checkout_key IS NULL", cartID).
			UpdateColumn("checkout_key", candidate)
		if result.Error != nil {
			// The only expected retryable failure is an astronomically unlikely
			// collision with another cart's unique key. Reading below also handles
			// a concurrent initializer that committed while this update waited.
			var cart Cart
			if err := s.p.engine.DB.Select("id", "checkout_key").First(&cart, cartID).Error; err == nil && cart.CheckoutKey != nil {
				return *cart.CheckoutKey, nil
			}
			continue
		}
		var cart Cart
		if err := s.p.engine.DB.Select("id", "checkout_key").First(&cart, cartID).Error; err != nil {
			return "", fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		if cart.CheckoutKey != nil && validCheckoutKey(*cart.CheckoutKey) {
			return *cart.CheckoutKey, nil
		}
	}
	return "", ErrCartUnavailable
}

// checkoutSnapshot returns cart lines and the active key from one stable cart
// revision. Rendering cannot simply View then install a key: a mutation between
// those operations would bind the new key to totals already rendered from the
// old cart. The final key re-read closes that window; a later mutation is also
// safe because it clears the key and POST revalidates it under the cart lock.
func (s *CartService) checkoutSnapshot(c *gin.Context) (CartView, string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		before := s.View(c)
		if before.Empty || before.cartID == 0 {
			return before, "", ErrEmptyCart
		}
		key, err := s.checkoutKey(before.cartID)
		if err != nil {
			return CartView{}, "", err
		}
		snapshot := s.View(c)
		if snapshot.Empty || snapshot.cartID == 0 {
			return snapshot, "", ErrEmptyCart
		}
		if snapshot.cartID != before.cartID || !checkoutKeysEqual(snapshot.checkoutKey, key) {
			continue
		}
		var current Cart
		if err := s.p.engine.DB.Select("id", "checkout_key").First(&current, snapshot.cartID).Error; err != nil {
			return CartView{}, "", fmt.Errorf("%w: %v", ErrCartUnavailable, err)
		}
		if current.CheckoutKey != nil && checkoutKeysEqual(*current.CheckoutKey, key) {
			return snapshot, key, nil
		}
	}
	return CartView{}, "", ErrCartUnavailable
}

// invalidateCartCheckoutKey clears a cart's active key. When expected is
// non-empty, the compare-and-clear preserves a newer key installed after a
// concurrent cart mutation; ordinary mutations pass an empty expected value
// and always invalidate the current snapshot.
func invalidateCartCheckoutKey(db *gorm.DB, cartID uint, expected string) error {
	query := db.Model(&Cart{}).Where("id = ?", cartID)
	if expected != "" {
		query = query.Where("checkout_key = ?", expected)
	}
	if err := query.UpdateColumn("checkout_key", nil).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	return nil
}

// Count returns the total item quantity in the current cart (for the nav badge),
// without creating a cart.
func (s *CartService) Count(c *gin.Context) int {
	cart := s.resolve(c, false)
	if cart == nil {
		return 0
	}
	var res struct{ Total int64 }
	s.p.engine.DB.Model(&CartItem{}).Where("cart_id = ?", cart.ID).
		Select("COALESCE(SUM(qty),0) AS total").Scan(&res)
	if res.Total <= 0 {
		return 0
	}
	if res.Total > int64(math.MaxInt) {
		return math.MaxInt
	}
	return int(res.Total)
}

type cartProduct struct {
	data     *ProductData
	price    int64
	currency string
	title    string
	url      string
}

func (p *cartProduct) maxQty() int {
	if p != nil && p.data != nil && p.data.ManageStock && p.data.StockQty < maxCartItemQty {
		if p.data.StockQty < 0 {
			return 0
		}
		return p.data.StockQty
	}
	return maxCartItemQty
}

// loadProduct derives cart pricing from the authoritative product_data row,
// never from a client value or a possibly stale lookup/cache row.
func (s *CartService) loadProduct(productID uint) (*cartProduct, error) {
	if productID == 0 {
		return nil, ErrProductUnavailable
	}
	if s == nil || s.p == nil || s.p.engine == nil || s.p.engine.Content == nil || s.p.engine.DB == nil {
		return nil, ErrCartUnavailable
	}
	item, err := s.p.engine.Content.FindByID(productID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductUnavailable
		}
		return nil, fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	if err := validateProductContent(item, time.Now().UTC()); err != nil {
		return nil, err
	}

	repo := s.p.repo
	if repo == nil {
		repo = NewRepository(s.p.engine.DB)
	}
	pd, err := repo.GetProductData(productID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductDataMissing
		}
		return nil, fmt.Errorf("%w: %v", ErrCartUnavailable, err)
	}
	price, currency, err := validateProductData(pd, s.p.storeCurrency())
	if err != nil {
		return nil, err
	}

	product := &cartProduct{data: pd, price: price, currency: currency, title: item.Title}
	if s.p.engine.Rewrite != nil {
		product.url = s.p.engine.Rewrite.BuildURL("product", item.Slug)
	}
	return product, nil
}

// validateCheckoutLine locks and revalidates every authoritative product row
// inside the order transaction. Cart.View is only a UI snapshot; an admin may
// change price/publication/product_data after it returns. Checkout must abort
// rather than charge the old amount or create an order for an unavailable row.
func (s *CartService) validateCheckoutLine(tx *gorm.DB, line CartLine, currency string, now time.Time) error {
	if line.ProductID == 0 {
		return ErrProductUnavailable
	}
	var item corecontent.Content
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, line.ProductID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProductUnavailable
		}
		return err
	}
	if err := validateProductContent(&item, now); err != nil {
		return err
	}
	var pd ProductData
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pd, "content_id = ?", line.ProductID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProductDataMissing
		}
		return err
	}
	price, _, err := validateProductData(&pd, currency)
	if err != nil {
		return err
	}
	lineTotal, err := cartLineTotal(price, line.Qty)
	if err != nil {
		return err
	}
	if price != line.UnitPrice || lineTotal != line.LineTotal {
		return ErrCheckoutCartChanged
	}
	return nil
}

func validateProductContent(item *corecontent.Content, now time.Time) error {
	if item == nil || item.Type != "product" || item.Status != corecontent.StatusPublished {
		return ErrProductUnavailable
	}
	if item.PublishedAt != nil && item.PublishedAt.After(now) {
		return ErrProductUnavailable
	}
	return nil
}

func validateProductData(pd *ProductData, storeCurrency string) (int64, string, error) {
	if pd == nil {
		return 0, "", ErrProductDataMissing
	}
	currency := strings.TrimSpace(pd.Currency)
	wantCurrency := strings.TrimSpace(storeCurrency)
	if currency == "" || len(currency) > 8 || wantCurrency == "" ||
		!strings.EqualFold(currency, wantCurrency) {
		return 0, "", ErrProductCurrencyMismatch
	}
	if pd.PriceAmount <= 0 || pd.PriceAmount > maxCartUnitPrice {
		return 0, "", ErrInvalidProductPrice
	}
	price := pd.PriceAmount
	if pd.SalePriceAmount != nil {
		if *pd.SalePriceAmount <= 0 || *pd.SalePriceAmount >= pd.PriceAmount {
			return 0, "", ErrInvalidProductPrice
		}
		price = *pd.SalePriceAmount
	}
	if pd.ManageStock && (pd.StockStatus != "instock" || pd.StockQty <= 0) {
		return 0, "", ErrInsufficientStock
	}
	return price, strings.ToUpper(currency), nil
}

func validateCartQuantity(qty int) error {
	if qty < 1 || qty > maxCartItemQty {
		return ErrInvalidCartQuantity
	}
	return nil
}

func cartLineTotal(price int64, qty int) (int64, error) {
	if err := validateCartQuantity(qty); err != nil {
		return 0, err
	}
	if price <= 0 || price > math.MaxInt64/int64(qty) {
		return 0, ErrInvalidProductPrice
	}
	return price * int64(qty), nil
}

func (s *CartService) setCookie(c *gin.Context, token string) {
	secure := false
	if s.p.engine != nil && s.p.engine.Config != nil {
		secure = strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.p.engine.Config.Site.URL)), "https://")
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cartCookie, token, 86400*30, "/", "", secure, true)
}

func (s *CartService) clearCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cartCookie, "", -1, "/", "", false, true)
}

func newCartToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
