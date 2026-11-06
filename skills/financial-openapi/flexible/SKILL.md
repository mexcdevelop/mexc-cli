---
name: mexc-financial-openapi-flexible
scope: financial-openapi.flexible
signed: yes
identity: yes
visibility: public
description: 'Flexible (demand) financial operations via openapi gateway — auto-pledge management and queries for products, orders, profit records, and positions. Triggers: "flexible financial products","flexible financial auto pledge","flexible financial orders".'
---

> Auth: this subdomain requires an openapi identity (api-key auth). The account is resolved from the request's own headers, never from a parameter. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

# financial-openapi · flexible

Flexible (demand) financial operations exposed to clients through the openapi gateway under `/api/v3/financial/cli/flexible`.

## Core Concepts

Flexible (demand) financial products offer instant liquidity with variable returns. This subdomain provides auto-pledge management and comprehensive query capabilities.

**Key entities:**
- **Product**: Flexible financial product with variable APR
- **Pledge Order**: Investment order into a flexible product
- **Redeem Order**: Withdrawal order from a flexible product
- **Profit Record**: Earnings distribution records
- **Position Detail**: User's holdings in flexible products
- **Auto-Pledge**: Automatic investment configuration per currency
- **APR History**: Historical APR data for products

**API Characteristics:**
- **Auto-pledge management**: Toggle auto-pledge per currency or globally
- **Product queries**: Detail and APR history
- **Order queries**: Paginated pledge/redeem order lists
- **Profit queries**: Paginated profit records with summary
- **Position queries**: Detailed position breakdown

## Important Notes

- **Auto-pledge settings are writes** (non-idempotent)
- memberId is **extracted from header** (not passed as parameter)
- History `startTime` and `endTime` are optional 13-digit UTC Unix timestamps in milliseconds; when provided, each value must be numeric.
- Flexible products have **instant liquidity** (redeem anytime)
- Auto-pledge can be configured **per currency** or **globally**
- This is a public OpenAPI subdomain with openapi identity

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc financial-openapi flexible auto-pledge-setting` | `financial-openapi.flexible` | write | yes | yes | no | `GET /api/v3/financial/cli/flexible/auto_pledge/setting` | Set global auto-pledge switch |
| `mexc financial-openapi flexible auto-pledge-setting-query` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/auto_pledge/setting/query` | Query global auto-pledge state |
| `mexc financial-openapi flexible auto-pledge-financial-setting` | `financial-openapi.flexible` | write | yes | yes | no | `POST /api/v3/financial/cli/flexible/auto_pledge/financial/setting` | Set auto-pledge per currency (batch) |
| `mexc financial-openapi flexible auto-pledge-financial-list` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/auto_pledge/financial_list` | Query auto-pledge config list |
| `mexc financial-openapi flexible member-pledge-orders-page` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/pledge_orders/page` | Query paginated pledge orders |
| `mexc financial-openapi flexible member-redeemable-list` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/redeemable/list` | Query redeemable products |
| `mexc financial-openapi flexible member-redeem-orders-page` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/redeem_orders/page` | Query paginated redeem orders |
| `mexc financial-openapi flexible member-profit-record-page` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/profit_record/page` | Query paginated profit records |
| `mexc financial-openapi flexible apr-history` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/apr/history` | Query APR history |
| `mexc financial-openapi flexible detail` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/detail` | Query product detail |
| `mexc financial-openapi flexible member-pledge-currency` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/pledge_orders/currency` | Query pledge currencies |
| `mexc financial-openapi flexible member-redeem-currency` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/redeem_orders/currency` | Query redeem currencies |
| `mexc financial-openapi flexible member-profit-currency` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/profit_record/currency` | Query profit currencies |
| `mexc financial-openapi flexible auto-pledge-apr` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/auto_pledge/apr` | Query max auto-pledge APR |
| `mexc financial-openapi flexible auto-pledge-kyc-level` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/auto_pledge/kyc_level` | Query user auto-pledge KYC level |
| `mexc financial-openapi flexible member-position-detail` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/position_detail` | Query position detail |
| `mexc financial-openapi flexible member-profit-record-summary` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/profit_record_summary` | Query profit summary |
| `mexc financial-openapi flexible member-interest-detail` | `financial-openapi.flexible` | read | yes | yes | yes | `GET /api/v3/financial/cli/flexible/member/interest_detail` | Query interest detail |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| auto-pledge-setting / auto-pledge-financial-setting | `financial-openapi.flexible` | write |
| auto-pledge-setting-query / auto-pledge-financial-list / member-pledge-orders-page / member-redeemable-list / member-redeem-orders-page / member-profit-record-page / apr-history / detail / member-pledge-currency / member-redeem-currency / member-profit-currency / auto-pledge-apr / auto-pledge-kyc-level / member-position-detail / member-profit-record-summary / member-interest-detail | `financial-openapi.flexible` | read |

## Usage Examples

```bash
# Enable global auto-pledge
mexc financial-openapi flexible auto-pledge-setting --autoPledge 1

# Query global auto-pledge state
mexc financial-openapi flexible auto-pledge-setting-query

# Set auto-pledge per currency (batch)
mexc financial-openapi flexible auto-pledge-financial-setting --data '[
  {"currencyId": "8c9d9272b7834a3db22ee5b549d42588", "autoPledgeFlag": 1},
  {"currencyId": "abc123", "autoPledgeFlag": 0}
]'

# Query auto-pledge config list
mexc financial-openapi flexible auto-pledge-financial-list --currency BTC --showOpenedOnly true

# Query product detail
mexc financial-openapi flexible detail --financialId "FIN001"

# Query APR history
mexc financial-openapi flexible apr-history --financialId "FIN001"

# Query paginated pledge orders
mexc financial-openapi flexible member-pledge-orders-page --page 1 --pageSize 20 --currencyId "8c9d9272b7834a3db22ee5b549d42588" --startTime 1783900800000 --endTime 1786579199000

# Query position detail
mexc financial-openapi flexible member-position-detail --financialNo "FIN001"

# Query interest detail
mexc financial-openapi flexible member-interest-detail --profitRecordNo "REC001" --page 1 --pageSize 20
```

## Workflows

### Review and update auto-pledge settings
> User: "Check my auto-pledge configuration, then enable it for a currency."
1. `mexc financial-openapi flexible auto-pledge-setting-query` → read the current global auto-pledge state.
2. `mexc financial-openapi flexible auto-pledge-financial-list --currency <currency>` → inspect the current per-currency configuration.
3. Confirm the intended change, then use `mexc financial-openapi flexible auto-pledge-setting --autoPledge 1` for the global setting or `mexc financial-openapi flexible auto-pledge-financial-setting --data '[{"currencyId":"<currencyId>","autoPledgeFlag":1}]'` for one currency.
   ↓ step 3 is a write: show the user a summary and wait for explicit confirmation before running.
4. `mexc financial-openapi flexible auto-pledge-setting-query` and `mexc financial-openapi flexible auto-pledge-financial-list` → re-query to verify the saved configuration.

### Filter profit records by currency
> User: "Show my flexible earn profit for BTC in the last month."
1. `mexc financial-openapi flexible member-profit-currency` → retrieve available currencies for the filter dropdown.
2. `mexc financial-openapi flexible member-profit-record-page --currencyId <currencyId from step 1> --startTime <startTime> --endTime <endTime> --page 1 --pageSize 20` → query paginated profit records filtered by selected currency and time range.

### Inspect position detail with APR history
> User: "Show details of my flexible holding and its APR trend."
1. `mexc financial-openapi flexible member-position-detail --financialNo <financialNo>` → query the position breakdown for a specific product.
2. `mexc financial-openapi flexible apr-history --financialId <financialId>` → retrieve historical APR data for the same product to assess rate stability.

## Parameter Validation Rules

### auto-pledge-setting
- `autoPledge` (**required**): Auto-pledge switch, `0` = disable, `1` = enable

### auto-pledge-financial-setting (one entry of the batch)
- `currencyId` (**required**): Currency ID (string, not null)
- `autoPledgeFlag` (**required**): Auto-pledge switch (integer, not null), `0` = disable, `1` = enable

### auto-pledge-financial-list
- `currency` (optional): Currency name for fuzzy search
- `showOpenedOnly` (optional): Whether to show only enabled auto-pledge configs

### detail / apr-history
- `financialId` (**required**): Product ID

### auto-pledge-kyc-level
- `financialId` (optional): Product ID

### member-pledge-orders-page
- `currencyId` (optional): Currency ID
- `startTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### member-redeem-orders-page
- `currencyId` (optional): Currency ID
- `startTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### member-profit-record-page
- `currencyId` (optional): Currency ID
- `financialNo` (optional): Product number
- `startTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### member-position-detail
- `financialNo` (**required**): Product number

### member-interest-detail
- `profitRecordNo` (**required**): Profit record number (string, not null)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

## Notes for Agents

**General rules**
- Auto-pledge settings are write operations (non-idempotent)
- Show confirmation before executing writes
- memberId is extracted from request header, never passed as parameter
- All list queries are paginated

**Skill-specific notes**
- Flexible products offer instant liquidity (redeem anytime)
- Auto-pledge can be configured globally or per currency
- Global auto-pledge: user-level switch (affects all currencies)
- Per-currency auto-pledge: batch configuration for multiple currencies
- APR history shows historical rate changes for a product
- Position detail shows holdings breakdown per product
- Interest detail shows interest accrual breakdown per profit record
- Currency dropdown endpoints provide available currencies for filtering
