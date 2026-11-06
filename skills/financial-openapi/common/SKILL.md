---
name: mexc-financial-openapi-common
scope: financial-openapi.common
signed: yes
identity: yes
visibility: public
description: 'Common financial operations (flexible + fixed + contract-hold) exposed via openapi gateway — query products, member assets, positions, orders, and currencies. Triggers: "financial products","member assets","member positions","pledge orders","redeem orders","financial currencies".'
---

> Auth: this subdomain requires an openapi identity (api-key auth). The account is resolved from the request's own headers, never from a parameter. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

# financial-openapi · common

Common financial operations shared across flexible, fixed, and contract-hold products, exposed to clients through the openapi gateway under `/api/v3/financial/cli/common`.

## Core Concepts

This subdomain provides unified query interfaces for financial products across all types (flexible, fixed, contract-hold). It serves as a single entry point for users to discover products, manage positions, and track orders.

**Key entities:**
- **Product**: Financial product with properties like name, APR, currency, investment period
- **Member Asset**: User's total financial asset across all product types
- **Position**: User's holdings per currency or product dimension
- **Pledge Order**: Investment (stake) order into a financial product
- **Redeem Order**: Withdrawal order from a financial product
- **Currency**: Tradable currency with ID and name

**API Characteristics:**
- **Product discovery**: List products with filters (currency, period type, sort)
- **Asset queries**: Member total assets, positions by currency/product
- **Order queries**: Pledge and redeem orders with pagination
- **Currency dropdowns**: Available currencies for filtering orders

## Important Notes

- All operations are **read-only** (no writes)
- memberId is **extracted from header** (not passed as parameter)
- Product lists are non-paginated.
- `member-assets-v2` returns `totalAsset`, `totalProfit`, and `yesterdayProfit` as USDT-denominated string amounts. The response omits a currency field; callers must treat these three fields as USDT values.
- Order queries require both `startTime` and `endTime` as 13-digit UTC Unix timestamps in milliseconds.
- Product visibility is **member-specific** (based on eligibility)
- This is a public OpenAPI subdomain with openapi identity

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc financial-openapi common products-list-v2` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/products/list/V2` | Query product list (V2, non-paginated) |
| `mexc financial-openapi common products-max-apr` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/products/max_apr` | Query maximum APR across products |
| `mexc financial-openapi common products-by-currency` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/products/by_currency` | Query products by currency ID (for pledge popup) |
| `mexc financial-openapi common member-assets-v2` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/assets/v2` | Query member assets (V2, excludes on-chain earn) |
| `mexc financial-openapi common member-positions` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/positions` | Query member positions by currency |
| `mexc financial-openapi common member-positions-by-product-v2` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/positions_by_product/v2` | Query positions by product (V2, batch) |
| `mexc financial-openapi common member-recommend` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/recommend` | Query recommended products for member |
| `mexc financial-openapi common member-pledge-orders` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/pledge_orders` | Query member pledge orders |
| `mexc financial-openapi common member-redeem-orders` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/redeem_orders` | Query member redeem orders |
| `mexc financial-openapi common member-pledge-currency` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/pledge_orders/currency` | Query pledge order currencies |
| `mexc financial-openapi common member-redeem-currency` | `financial-openapi.common` | read | yes | yes | yes | `GET /api/v3/financial/cli/common/member/redeem_orders/currency` | Query redeem order currencies |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| All operations (products-list-v2 / products-max-apr / products-by-currency / member-assets-v2 / member-positions / member-positions-by-product-v2 / member-recommend / member-pledge-orders / member-redeem-orders / member-pledge-currency / member-redeem-currency) | `financial-openapi.common` | read |

## Usage Examples

```bash
# Query product list
mexc financial-openapi common products-list-v2 --currency BTC --investPeriodType FLEXIBLE

# Query products by currency (for pledge popup)
mexc financial-openapi common products-by-currency --currencyId "8c9d9272b7834a3db22ee5b549d42588"

# Query member assets
mexc financial-openapi common member-assets-v2

# Query member positions by currency (with filters)
mexc financial-openapi common member-positions --currency BTC --hideSmall true

# Query pledge orders (13-digit UTC Unix milliseconds)
mexc financial-openapi common member-pledge-orders --startTime 1783900800000 --endTime 1786579199000

# Query pledge order currencies (for dropdown)
mexc financial-openapi common member-pledge-currency
```

## Workflows

### Discover products, then inspect a member's position
> User: "Which flexible products are available, and what does this member already hold?"
1. `mexc financial-openapi common products-list-v2 --currency <currency> --investPeriodType FLEXIBLE` → discover eligible flexible products and their currency IDs.
2. `mexc financial-openapi common member-assets-v2` → inspect the member's total financial assets overview.
3. `mexc financial-openapi common member-positions --currency <currency>` → inspect the member's current holdings for that currency.
4. When history is needed, call `mexc financial-openapi common member-pledge-orders --currencyId <currencyId> --startTime <startTime> --endTime <endTime>` using the currency ID from the product result.

### View positions and get recommendations when empty
> User: "Show my earn holdings; suggest products if I have none."
1. `mexc financial-openapi common member-positions-by-product-v2` → query positions across all product types (flexible + fixed). If the result is non-empty, stop here.
2. `mexc financial-openapi common member-recommend` → when step 1 returns an empty list, fetch personalized product recommendations for the member.

### Filter orders by currency
> User: "Show my pledge orders for BTC in the last month."
1. `mexc financial-openapi common member-pledge-currency` (or `member-redeem-currency` for redeem orders) → retrieve available currencies for the dropdown filter.
2. `mexc financial-openapi common member-pledge-orders --currencyId <currencyId from step 1> --startTime <startTime> --endTime <endTime>` → query orders filtered by the selected currency and time range. Both `startTime` and `endTime` are required for order queries.

## Parameter Validation Rules

### products-list-v2
- `currency` (optional): Currency name for fuzzy search
- `investPeriodType` (optional): Investment period type, values: `FLEXIBLE` (flexible) / `FIXED` (fixed)
- `matchMemberAssets` (optional): Whether to match member's assets
- `sortField` (optional): Sort field, `1` = estimated APR
- `asc` (optional): Sort direction, `true` = ascending, `false` = descending

### member-pledge-orders / member-redeem-orders
- `currencyId` (optional): Currency ID
- `financialType` (optional): Financial type, empty means all
- `startTime` (**required**): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (**required**): UTC Unix timestamp in milliseconds (exactly 13 digits)

### products-by-currency
- `currencyId` (**required**): Currency ID

### member-positions
- `currency` (optional): Currency name for fuzzy search
- `hideSmall` (optional): Whether to hide small assets (<1 USD), default `false`

### member-positions-by-product-v2
- `financialTypes` (optional): Financial type list, values: `FLEXIBLE` / `FIXED` / `BLC_EARN`

### member-recommend
- `financialType` (optional): Financial type, values: `FLEXIBLE` / `FIXED` / `BLC_EARN`

## Notes for Agents

**General rules**
- All operations are read-only and idempotent
- memberId is extracted from request header, never passed as parameter
- Time range filters are required for common pledge and redeem order queries.
- Pagination uses `page` and `pageSize`, not `size`
- Currency filtering can use either `currency` (name) or `currencyId` (ID)

**Skill-specific notes**
- This subdomain provides unified interfaces across all financial product types
- Product visibility is member-specific based on eligibility rules
- Positions can be queried by currency dimension or product dimension
- Order queries support time range filtering and currency filtering
- Currency dropdown endpoints provide available currencies for filtering
