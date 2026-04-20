// Package models defines OpenRTB 2.5 data structures used by the SSP ad server.
// These structs cover the subset of the specification required for bid request
// parsing, validation, and bid response generation.
package models

// BidRequest represents an OpenRTB 2.5 bid request sent by a publisher
// to solicit bids from demand-side platforms.
type BidRequest struct {
	// ID is the unique identifier for this bid request, provided by the exchange.
	ID string `json:"id" validate:"required"`
	// Imp is the list of Impression objects representing ad slots available for bidding.
	Imp []Impression `json:"imp" validate:"required,min=1,dive"`
	// Site describes the publisher's website where the ad will be displayed.
	Site *Site `json:"site,omitempty"`
	// App describes the publisher's application (mutually exclusive with Site).
	App *App `json:"app,omitempty"`
	// Device provides information about the user's device.
	Device *Device `json:"device,omitempty"`
	// User describes the human user of the device; the advertising audience.
	User *User `json:"user,omitempty"`
	// Test indicates whether this is a test request (1 = test, 0 = live).
	Test int `json:"test,omitempty"`
	// AuctionType defines the auction method (1 = First Price, 2 = Second Price+).
	AuctionType int `json:"at,omitempty" validate:"omitempty,oneof=1 2"`
	// TMax is the maximum time in milliseconds the exchange allows for bids.
	TMax int `json:"tmax,omitempty"`
	// Cur is the list of allowed currencies for bids using ISO-4217 codes.
	Cur []string `json:"cur,omitempty"`
}

// Impression represents a single ad placement opportunity within a bid request.
type Impression struct {
	// ID is the unique identifier for this impression within the request.
	ID string `json:"id" validate:"required"`
	// Banner describes a banner ad slot if the impression supports display ads.
	Banner *Banner `json:"banner,omitempty"`
	// BidFloor is the minimum CPM bid the publisher will accept.
	BidFloor float64 `json:"bidfloor,omitempty" validate:"omitempty,gte=0"`
	// BidFloorCur is the ISO-4217 currency of the bid floor (default: USD).
	BidFloorCur string `json:"bidfloorcur,omitempty"`
	// Secure indicates if the impression requires HTTPS creative assets (1 = yes).
	Secure *int `json:"secure,omitempty"`
}

// Banner describes the properties of a display banner ad slot.
type Banner struct {
	// W is the exact width of the banner in device-independent pixels.
	W int `json:"w,omitempty" validate:"omitempty,gt=0"`
	// H is the exact height of the banner in device-independent pixels.
	H int `json:"h,omitempty" validate:"omitempty,gt=0"`
	// Pos is the ad position on screen (see OpenRTB 2.5 §5.4).
	Pos int `json:"pos,omitempty"`
	// MIMEs is the list of supported content MIME types (e.g. "image/jpeg").
	MIMEs []string `json:"mimes,omitempty"`
}

// Device provides information about the user's device making the ad request.
type Device struct {
	// UA is the browser user agent string.
	UA string `json:"ua,omitempty"`
	// IP is the IPv4 address closest to the device.
	IP string `json:"ip,omitempty" validate:"omitempty,ip"`
	// Geo contains geographic location data derived from the device.
	Geo *Geo `json:"geo,omitempty"`
	// Language is the browser language using ISO-639-1-alpha-2.
	Language string `json:"language,omitempty"`
	// OS is the operating system of the device (e.g. "iOS", "Android").
	OS string `json:"os,omitempty"`
	// DeviceType is the OpenRTB device type (see §5.21).
	DeviceType int `json:"devicetype,omitempty"`
}

// Geo contains geographic location information associated with a device.
type Geo struct {
	// Lat is the latitude from -90.0 to 90.0 (south is negative).
	Lat float64 `json:"lat,omitempty"`
	// Lon is the longitude from -180.0 to 180.0 (west is negative).
	Lon float64 `json:"lon,omitempty"`
	// Country is the ISO-3166-1-alpha-3 country code.
	Country string `json:"country,omitempty"`
	// City is the city of the user.
	City string `json:"city,omitempty"`
}

// User describes the human user of the device; the advertising audience.
type User struct {
	// ID is the exchange-specific user identifier.
	ID string `json:"id,omitempty"`
	// BuyerUID is the buyer-specific user identifier mapped by the exchange.
	BuyerUID string `json:"buyeruid,omitempty"`
	// Gender is the gender of the user ("M" = male, "F" = female, "O" = other).
	Gender string `json:"gender,omitempty" validate:"omitempty,oneof=M F O"`
	// YOB is the year of birth as a four-digit integer.
	YOB int `json:"yob,omitempty"`
}

// Site describes the publisher's website where the impression will be shown.
type Site struct {
	// ID is the exchange-specific site identifier.
	ID string `json:"id,omitempty"`
	// Name is the site name (may be aliased for publisher privacy).
	Name string `json:"name,omitempty"`
	// Domain is the domain of the site (e.g. "example.com").
	Domain string `json:"domain,omitempty"`
	// Page is the full URL of the page where the ad will be shown.
	Page string `json:"page,omitempty"`
	// Publisher describes the publisher of the site.
	Publisher *Publisher `json:"publisher,omitempty"`
}

// App describes the publisher's application in mobile contexts.
type App struct {
	// ID is the exchange-specific app identifier.
	ID string `json:"id,omitempty"`
	// Name is the app name.
	Name string `json:"name,omitempty"`
	// Bundle is the application bundle or package name (e.g. com.foo.bar).
	Bundle string `json:"bundle,omitempty"`
	// Publisher describes the publisher of the app.
	Publisher *Publisher `json:"publisher,omitempty"`
}

// Publisher describes the publisher of the site or app.
type Publisher struct {
	// ID is the exchange-specific publisher identifier.
	ID string `json:"id,omitempty"`
	// Name is the publisher name.
	Name string `json:"name,omitempty"`
}

// BidResponse is the top-level response object returned by the SSP containing
// one or more seat bids.
type BidResponse struct {
	// ID matches the BidRequest.ID to which this is a response.
	ID string `json:"id"`
	// SeatBid is the collection of bids grouped by buyer seat.
	SeatBid []SeatBid `json:"seatbid,omitempty"`
	// Cur is the ISO-4217 currency of all bid prices in this response.
	Cur string `json:"cur,omitempty"`
	// NBR represents the No Bid Reason.
	NBR int `json:"nbr,omitempty"`
}

// SeatBid groups bids associated with a single buyer seat.
type SeatBid struct {
	// Bid is the list of individual bid objects.
	Bid []Bid `json:"bid"`
	// Seat is the identifier of the buyer seat on whose behalf this bid is made.
	Seat string `json:"seat,omitempty"`
}

// Bid represents a single bid offer for a specific impression.
type Bid struct {
	// ID is the bidder-generated bid identifier for logging and tracking.
	ID string `json:"id"`
	// ImpID is the ID of the Impression object to which this bid applies.
	ImpID string `json:"impid"`
	// Price is the bid price expressed as CPM.
	Price float64 `json:"price"`
	// AdID is the identifier of the pre-loaded ad to serve if the bid wins.
	AdID string `json:"adid,omitempty"`
	// NURL is the win notice URL called by the exchange when the bid wins.
	NURL string `json:"nurl,omitempty"`
	// AdMarkup is the ad markup (HTML/VAST/etc.) to serve if the bid wins.
	AdMarkup string `json:"adm,omitempty"`
	// CrID is the creative identifier for reporting.
	CrID string `json:"crid,omitempty"`
	// W is the width of the creative in device-independent pixels.
	W int `json:"w,omitempty"`
	// H is the height of the creative in device-independent pixels.
	H int `json:"h,omitempty"`
	// Ext contains bidder-specific extension data.
	Ext map[string]interface{} `json:"ext,omitempty"`
}
