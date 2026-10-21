package seed

import "github.com/KING-CYBERTON/ayopa/internal/store"

const (
	mallName        = "Ayopa Dev Mall"
	mallTagline     = "Four local shops, one address"
	mallDescription = "Sample shops for trying the storefront layouts, palettes and blog. " +
		"Everything here is test data and is rebuilt each time the seed runs."
)

type shop struct {
	Subdomain  string
	Name       string
	Profile    store.TenantProfile
	Style      string
	Palette    string
	Theme      string
	Staff      bool
	Categories []string
	Products   []product
	Posts      []post
}

type product struct {
	Name        string
	Category    string // empty leaves it uncategorised
	Description string
	Price       string // in major units, for example "1250.00"
	Stock       int
	Status      string
	Featured    bool
}

type post struct {
	Title    string
	Excerpt  string
	Category string
	Status   string
	Body     string // {{Product name}} becomes that product's /shop/ID link
}

const (
	pub   = store.ProductPublished
	draft = store.ProductDraft
	arch  = store.ProductArchived
)

// The shops cover every layout and palette, the three colour modes and a
// zero-decimal currency, plus products that are low on stock, sold out,
// drafted, archived and uncategorised.
var shops = []shop{
	{
		Subdomain: "nyumba",
		Name:      "Nyumba Home & Living",
		Profile: store.TenantProfile{
			Tagline:      "Handmade homeware from Kenyan makers",
			Description:  "Ceramics, woven baskets and textiles made in small batches by workshops in Nairobi, Machakos and Kisumu.",
			Phone:        "+254 712 345 678",
			Address:      "Shop 14, The Mall, Westlands, Nairobi",
			OpeningHours: "Mon–Sat 9:00–18:00\nSun 11:00–16:00",
			Currency:     "KES",
		},
		Style: "boutique", Palette: "terracotta", Theme: "light",
		Staff:      true,
		Categories: []string{"Ceramics", "Baskets", "Textiles", "Lighting"},
		Products: []product{
			{"Speckled stoneware mug", "Ceramics", "Wheel-thrown stoneware with a speckled oat glaze. Holds 350 ml. Dishwasher safe.", "1450.00", 24, pub, true},
			{"Terracotta serving bowl", "Ceramics", "A wide, shallow bowl in unglazed terracotta with a burnished rim. 28 cm across.", "3200.00", 6, pub, false},
			{"Set of four side plates", "Ceramics", "Four 20 cm plates in a soft white glaze with a raw clay edge.", "4800.00", 3, pub, false},
			{"Kiondo market basket", "Baskets", "Sisal kiondo woven in Machakos, with leather handles. Each one is a little different.", "2600.00", 15, pub, true},
			{"Lidded storage basket", "Baskets", "Tight-weave basket with a fitted lid, for blankets or toys. 40 cm tall.", "5400.00", 0, pub, false},
			{"Kikoi throw", "Textiles", "Handloomed cotton kikoi in rust and indigo stripes, finished with knotted tassels. 160 × 100 cm.", "3900.00", 11, pub, true},
			{"Mudcloth cushion cover", "Textiles", "Cotton cover with a hand-painted mudcloth pattern. 45 × 45 cm, insert not included.", "2200.00", 2, pub, false},
			{"Woven pendant shade", "Lighting", "A raffia pendant shade, 50 cm across. Fits a standard E27 fitting.", "6800.00", 4, pub, false},
			{"Gift card", "", "A gift card for any amount up to the value shown. Delivered by email.", "5000.00", 100, pub, false},
			{"Indigo table runner", "Textiles", "Coming in December: a shibori-dyed runner from our Kisumu workshop.", "2900.00", 0, draft, false},
			{"Beaded coasters (old stock)", "Ceramics", "Discontinued last season.", "800.00", 0, arch, false},
		},
		Posts: []post{
			{
				Title:    "Meet the potters behind our mugs",
				Excerpt:  "A morning at the Machakos workshop where every mug in the shop is thrown, trimmed and glazed by hand.",
				Category: "Makers",
				Status:   pub,
				Body: "Most of our ceramics come from a four-person workshop just outside Machakos. We spent a morning there in September.\n\n" +
					"## From clay to kiln\n\n" +
					"The clay is dug locally, sieved and left to age for a few weeks before it is wedged and thrown. A good thrower makes about forty mugs a day.\n\n" +
					"The speckles in our best-selling mug come from iron in the clay bleeding through the glaze. No two come out quite the same.\n\n" +
					"## Take one home\n\n" +
					"Shop the {{Speckled stoneware mug}} or pair it with the {{Set of four side plates}}.",
			},
			{
				Title:    "How to care for a sisal basket",
				Excerpt:  "Three simple habits that keep a kiondo looking good for years.",
				Category: "Care guides",
				Status:   pub,
				Body: "Sisal is tough, but it does not like being soaked.\n\n" +
					"Brush off dust with a soft brush. If it gets wet, reshape it and let it dry in the shade, never in direct sun.\n\n" +
					"Leather handles like a little wax once a year. Our {{Kiondo market basket}} ships with a small tin.",
			},
			{
				Title:    "Holiday gift guide (draft)",
				Excerpt:  "Not published yet, so it should only show in the admin.",
				Category: "Guides",
				Status:   draft,
				Body:     "Ideas for under KES 3,000, coming soon.",
			},
		},
	},
	{
		Subdomain: "sokoni",
		Name:      "Sokoni Fresh",
		Profile: store.TenantProfile{
			Tagline:      "Farm produce delivered across Nairobi",
			Description:  "Fruit, vegetables and pantry staples from smallholder farms in Kiambu and Murang'a. Order by 10am for same-day delivery.",
			Phone:        "+254 722 000 111",
			Address:      "Stall 7, City Market, Muindi Mbingu Street, Nairobi",
			OpeningHours: "Every day 6:30–19:00",
			Currency:     "KES",
		},
		Style: "market", Palette: "forest", Theme: "auto",
		Categories: []string{"Fruit", "Vegetables", "Pantry", "Drinks", "Dairy & eggs"},
		Products: []product{
			{"Hass avocados (4 pack)", "Fruit", "Ripe-ready Hass avocados from Murang'a.", "320.00", 60, pub, true},
			{"Sweet bananas (1 kg)", "Fruit", "Small, sweet ndizi from Kisii.", "150.00", 45, pub, false},
			{"Mangoes (3 pack)", "Fruit", "Apple mangoes in season. Sold out until the next harvest.", "270.00", 0, pub, false},
			{"Passion fruit (500 g)", "Fruit", "Purple passion fruit, great for juice.", "220.00", 5, pub, false},
			{"Sukuma wiki bunch", "Vegetables", "A generous bunch of fresh kale, picked the morning it ships.", "50.00", 120, pub, true},
			{"Tomatoes (1 kg)", "Vegetables", "Firm salad tomatoes.", "140.00", 80, pub, false},
			{"Red onions (1 kg)", "Vegetables", "Medium red onions, dried and sorted.", "130.00", 3, pub, false},
			{"Managu bunch", "Vegetables", "African nightshade, washed and bundled.", "80.00", 25, pub, false},
			{"Wild honey (500 g)", "Pantry", "Raw honey from Baringo hives, unfiltered.", "850.00", 18, pub, true},
			{"Stone-ground maize flour (2 kg)", "Pantry", "Whole-grain posho mill flour.", "210.00", 40, pub, false},
			{"Njahi beans (1 kg)", "Pantry", "Black beans, sorted and cleaned.", "260.00", 22, pub, false},
			{"Fresh passion juice (1 L)", "Drinks", "Pressed daily, no added sugar. Keep chilled.", "350.00", 12, pub, false},
			{"Free-range eggs (tray of 30)", "Dairy & eggs", "From pasture-raised kienyeji hens.", "690.00", 9, pub, true},
			{"Mursik (500 ml)", "Dairy & eggs", "Traditional fermented milk. Back next week.", "300.00", 0, draft, false},
		},
		Posts: []post{
			{
				Title:    "What's in season this month",
				Excerpt:  "Avocados are at their best, mangoes are nearly over and the first passion fruit has arrived.",
				Category: "Seasonal",
				Status:   pub,
				Body: "October is avocado month. Our Murang'a growers are picking every few days, so the {{Hass avocados (4 pack)}} you get will be ripe within two days.\n\n" +
					"## Running low\n\n" +
					"The mango season is nearly done. We have sold out of the {{Mangoes (3 pack)}} and expect the next harvest in December.",
			},
		},
	},
	{
		Subdomain: "kahawa",
		Name:      "Kahawa Roasters",
		Profile: store.TenantProfile{
			Tagline:      "Single-origin coffee, roasted in Kampala",
			Description:  "We buy green coffee directly from cooperatives on Mount Elgon and in the Rwenzori and roast twice a week.",
			Phone:        "+256 772 123 456",
			Address:      "Plot 22, Kisementi, Kampala",
			OpeningHours: "Tue–Sun 7:00–17:00",
			Currency:     "UGX",
		},
		Style: "classic", Palette: "ocean", Theme: "dark",
		Categories: []string{"Beans", "Brewing gear", "Gifts"},
		Products: []product{
			{"Mount Elgon washed arabica (250 g)", "Beans", "Bright and sweet, with notes of blackcurrant and brown sugar. Roasted medium-light.", "38000", 30, pub, true},
			{"Rwenzori natural (250 g)", "Beans", "Sun-dried cherries give a fruity, wine-like cup.", "42000", 14, pub, true},
			{"House espresso blend (1 kg)", "Beans", "Chocolate and toasted nut. Built for milk drinks.", "120000", 4, pub, false},
			{"Robusta decaf (250 g)", "Beans", "Swiss Water decaf from Masaka robusta.", "32000", 0, pub, false},
			{"Ceramic pour-over dripper", "Brewing gear", "Fits size 02 filters.", "65000", 8, pub, false},
			{"Hand grinder", "Brewing gear", "Steel burrs with 30 grind settings.", "185000", 2, pub, false},
			{"Paper filters (100)", "Brewing gear", "Unbleached size 02 filters.", "15000", 50, pub, false},
			{"Tasting flight gift box", "Gifts", "Three 100 g bags of this month's coffees with tasting notes.", "75000", 10, pub, false},
		},
		Posts: []post{
			{
				Title:    "Dialling in a pour-over",
				Excerpt:  "A simple recipe to start from, and what to change when the cup tastes sour or bitter.",
				Category: "Brew guides",
				Status:   pub,
				Body: "Start with 15 g of coffee to 250 g of water just off the boil.\n\n" +
					"## The recipe\n\n" +
					"Bloom with 40 g of water for 30 seconds, then pour slowly in circles until you reach 250 g. Aim to finish draining by three minutes.\n\n" +
					"## Adjusting\n\n" +
					"Sour and thin? Grind finer. Bitter and dry? Grind coarser.\n\n" +
					"We used the {{Ceramic pour-over dripper}} and the {{Mount Elgon washed arabica (250 g)}}.",
			},
			{
				Title:    "Visiting the Bugisu cooperative",
				Excerpt:  "Notes from harvest season on the slopes of Mount Elgon.",
				Category: "Origins",
				Status:   pub,
				Body:     "Harvest runs from August to December. Cherries are picked by hand, pulped the same day and fermented overnight before drying on raised beds.\n\nThe result is the cup we roast as {{Mount Elgon washed arabica (250 g)}}.",
			},
		},
	},
	{
		Subdomain: "mtindo",
		Name:      "Mtindo Studio",
		Profile: store.TenantProfile{
			Tagline:      "Everyday clothes cut from African prints",
			Description:  "Small-run shirts, dresses and accessories, designed and sewn in Dar es Salaam.",
			Phone:        "+255 754 987 654",
			Address:      "Msasani Peninsula, Dar es Salaam",
			OpeningHours: "Mon–Fri 10:00–18:00\nSat 10:00–14:00",
			Currency:     "TZS",
		},
		Style: "boutique", Palette: "violet", Theme: "dark",
		Categories: []string{"Shirts", "Dresses", "Accessories"},
		Products: []product{
			{"Kitenge camp-collar shirt", "Shirts", "Relaxed short-sleeve shirt in a bold wax print. Sizes S to XXL.", "65000.00", 18, pub, true},
			{"Kanga print linen shirt", "Shirts", "Long-sleeve linen shirt with a kanga-print yoke.", "78000.00", 1, pub, false},
			{"Wrap midi dress", "Dresses", "Adjustable wrap dress with flutter sleeves.", "120000.00", 7, pub, true},
			{"Shift dress", "Dresses", "A simple shift with pockets, lined to the hip.", "95000.00", 0, pub, false},
			{"Head wrap", "Accessories", "Two metres of cotton print, hemmed on all sides.", "25000.00", 40, pub, false},
			{"Tote bag", "Accessories", "Heavy canvas tote with a print lining.", "35000.00", 12, pub, false},
		},
	},
}
