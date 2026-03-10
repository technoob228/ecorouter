package accounts

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type ImpactResponse struct {
	UserEmail           string  `json:"user_email"`
	MemberSince         string  `json:"member_since"`
	TotalEcoContributed float64 `json:"total_eco_contributed_usdt"`
	BadgeURL            string  `json:"badge_url"`
	BadgeMD             string  `json:"badge_md"`
}

func getDomain() string {
	if d := os.Getenv("DOMAIN"); d != "" {
		return d
	}
	return "api.ecorouter.org"
}

func (s *Service) HandleImpact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ecoKey := extractBearerToken(r)
	if ecoKey == "" {
		http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
		return
	}

	user, err := s.db.GetUserByEcoKey(ecoKey)
	if err != nil {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
		return
	}

	domain := getDomain()
	userTag := fmt.Sprintf("USR_%d", user.ID)
	badgeURL := fmt.Sprintf("https://%s/badge/%s.svg", domain, userTag)
	verifyURL := fmt.Sprintf("https://ecorouter.org/verify/%s", userTag)
	badgeMD := fmt.Sprintf("[![EcoRouter](%s)](%s)", badgeURL, verifyURL)

	resp := ImpactResponse{
		UserEmail:           user.Email,
		MemberSince:         user.CreatedAt.Format("2006-01-02"),
		TotalEcoContributed: user.TotalEcoUSDT,
		BadgeURL:            badgeURL,
		BadgeMD:             badgeMD,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// parseUserIDFromPath extracts numeric user ID from paths like /badge/USR_1.svg or /verify/USR_1
func parseUserIDFromPath(path, prefix, suffix string) (int64, error) {
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)
	if !strings.HasPrefix(path, "USR_") {
		return 0, fmt.Errorf("invalid user tag")
	}
	idStr := strings.TrimPrefix(path, "USR_")
	return strconv.ParseInt(idStr, 10, 64)
}

func (s *Service) HandleBadge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := parseUserIDFromPath(r.URL.Path, "/badge/", ".svg")
	if err != nil {
		http.Error(w, "invalid badge path", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByID(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	amountStr := fmt.Sprintf("$%.2f contributed", user.TotalEcoUSDT)

	// Shields.io-style SVG badge
	leftText := "EcoRouter"
	rightText := amountStr

	// Approximate text widths (7px per char for the font size used)
	leftWidth := len(leftText)*7 + 10
	rightWidth := len(rightText)*7 + 10
	totalWidth := leftWidth + rightWidth

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <linearGradient id="s" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r">
    <rect width="%d" height="20" rx="3" fill="#fff"/>
  </clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="#16a34a"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="11">
    <text aria-hidden="true" x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
    <text aria-hidden="true" x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>`,
		totalWidth, leftText, rightText,
		leftText, rightText,
		totalWidth,
		leftWidth,
		leftWidth, rightWidth,
		totalWidth,
		leftWidth/2, leftText,
		leftWidth/2, leftText,
		leftWidth+rightWidth/2, rightText,
		leftWidth+rightWidth/2, rightText,
	)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(svg))
}

func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "***"
	}
	local := parts[0]
	if len(local) <= 1 {
		return local + "***@" + parts[1]
	}
	return string(local[0]) + "***@" + parts[1]
}

func (s *Service) HandleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := parseUserIDFromPath(r.URL.Path, "/verify/", "")
	if err != nil {
		http.Error(w, "invalid verify path", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByID(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	deposits, err := s.db.GetUserDepositHistory(userID)
	if err != nil {
		log.Printf("ERROR: get deposit history for user %d: %v", userID, err)
		deposits = nil
	}

	maskedEmail := html.EscapeString(maskEmail(user.Email))
	memberSince := user.CreatedAt.Format("2006-01-02")
	userTag := fmt.Sprintf("USR_%d", user.ID)

	// Build deposit rows
	var depositRows string
	for _, d := range deposits {
		txShort := d.TxHash
		if len(txShort) > 16 {
			txShort = txShort[:8] + "..." + txShort[len(txShort)-8:]
		}
		depositRows += fmt.Sprintf(`
            <tr>
                <td>%s</td>
                <td>$%.2f</td>
                <td>$%.2f</td>
                <td><a href="https://tronscan.org/#/transaction/%s" target="_blank" rel="noopener">%s</a></td>
            </tr>`,
			html.EscapeString(d.CreatedAt.Format("2006-01-02 15:04")),
			d.AmountUSDT,
			d.EcoFeeUSDT,
			html.EscapeString(d.TxHash),
			html.EscapeString(txShort),
		)
	}

	depositsSection := ""
	if len(deposits) > 0 {
		depositsSection = fmt.Sprintf(`
        <h2>Deposit History</h2>
        <table>
            <thead>
                <tr><th>Date</th><th>Amount</th><th>Eco Fee</th><th>Transaction</th></tr>
            </thead>
            <tbody>%s</tbody>
        </table>`, depositRows)
	} else {
		depositsSection = `<p style="color:#666; margin-top:24px;">No deposits yet.</p>`
	}

	pageHTML := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>EcoRouter — Verify %s</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Inter', sans-serif; color: #1a1a1a; background: #ffffff; line-height: 1.6; }
        code, .mono { font-family: 'JetBrains Mono', monospace; }
        a { color: #16a34a; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .container { max-width: 700px; margin: 0 auto; padding: 40px 24px; }
        .logo { font-size: 22px; font-weight: 700; margin-bottom: 32px; }
        .logo .eco { color: #16a34a; }
        .logo .router { color: #1a1a1a; }
        .badge-tag { display: inline-block; background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 6px; padding: 4px 12px; font-size: 14px; font-weight: 600; color: #16a34a; margin-bottom: 24px; }
        h1 { font-size: 24px; font-weight: 600; margin-bottom: 24px; }
        h2 { font-size: 18px; font-weight: 600; margin-bottom: 16px; margin-top: 32px; }
        .info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 8px; }
        .info-card { background: #f8faf9; border-radius: 8px; padding: 16px; }
        .info-label { font-size: 12px; color: #666; text-transform: uppercase; letter-spacing: 0.5px; }
        .info-value { font-size: 18px; font-weight: 600; margin-top: 4px; }
        .info-value.green { color: #16a34a; }
        table { width: 100%%; border-collapse: collapse; font-size: 14px; margin-top: 8px; }
        th { text-align: left; padding: 10px 12px; border-bottom: 2px solid #e5e7eb; font-weight: 500; color: #666; font-size: 13px; }
        td { padding: 10px 12px; border-bottom: 1px solid #f3f4f6; }
        td a { font-family: 'JetBrains Mono', monospace; font-size: 12px; }
        .disclaimer { margin-top: 40px; padding: 16px; background: #f8faf9; border-left: 3px solid #d1d5db; border-radius: 4px; font-size: 13px; color: #666; line-height: 1.7; }
        footer { margin-top: 40px; padding-top: 24px; border-top: 1px solid #e5e7eb; text-align: center; font-size: 13px; color: #999; }
        @media (max-width: 500px) {
            .info-grid { grid-template-columns: 1fr; }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo"><span class="eco">eco</span><span class="router">router</span></div>
        <span class="badge-tag">%s</span>
        <h1>Verified Ecological Contribution</h1>

        <div class="info-grid">
            <div class="info-card">
                <div class="info-label">Member</div>
                <div class="info-value">%s</div>
            </div>
            <div class="info-card">
                <div class="info-label">Member Since</div>
                <div class="info-value">%s</div>
            </div>
            <div class="info-card" style="grid-column: 1 / -1;">
                <div class="info-label">Total Eco Contributed</div>
                <div class="info-value green">$%.2f USDT</div>
            </div>
        </div>

        %s

        <div class="disclaimer">
            This is not a carbon offset certificate or tax receipt. It is a verifiable record of ecological contribution.
            All transactions can be independently verified on the Tron blockchain via Tronscan.
        </div>

        <footer>
            <a href="https://ecorouter.org">ecorouter.org</a> — Open source LLM proxy for the planet
        </footer>
    </div>
</body>
</html>`,
		html.EscapeString(userTag),
		html.EscapeString(userTag),
		maskedEmail,
		html.EscapeString(memberSince),
		user.TotalEcoUSDT,
		depositsSection,
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(pageHTML))
}
