---
name: mexc-financial-openapi-fixed
scope: financial-openapi.fixed
signed: yes
identity: yes
visibility: public
description: 'Fixed (term) financial query operations via openapi gateway — products, orders, profit records, and positions. Triggers: "fixed financial products","fixed financial orders","fixed financial profit records".'
---

> Auth: this subdomain requires an openapi identity (api-key auth). The account is resolved from the request's own headers, never from a parameter. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

# financial-openapi · fixed

Fixed (term) financial operations exposed to clients through the openapi gateway under `/api/v3/financial/cli/fixed`.

## Core Concepts

Fixed (term) financial products offer guaranteed returns over a specified investment period. This subdomain provides comprehensive query capabilities.

**Key entities:**
- **Product**: Fixed financial product with term, APR, currency, and limits
- **Pledge Order**: Investment order into a fixed product
- **Redeem Order**: Withdrawal order from a fixed product
- **Profit Record**: Earnings distribution records
- **Position Detail**: User's holdings in fixed products
- **Redeemable**: Products available for redemption

**API Characteristics:**
- **Product queries**: Detail, redeem detail, redeemable list
- **Order queries**: Paginated pledge/redeem order lists
- **Profit queries**: Paginated profit records with summary
- **Position queries**: Detailed position breakdown
- **Currency dropdowns**: Available currencies for filtering

## Important Notes

- memberId is **extracted from header** (not passed as parameter)
- All order queries are **paginated**
- History `startTime` and `endTime` are optional 13-digit UTC Unix timestamps in milliseconds; when provided, each value must be numeric.
- Fixed products have **maturity dates** (redemption rules apply)
- This is a public OpenAPI subdomain with openapi identity

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc financial-openapi fixed detail` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/detail` | Query product detail |
| `mexc financial-openapi fixed redeem-detail` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/redeem/detail` | Query redeem detail |
| `mexc financial-openapi fixed member-pledge-orders-page` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/pledge_orders/page` | Query paginated pledge orders |
| `mexc financial-openapi fixed member-redeem-orders-page` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/redeem_orders/page` | Query paginated redeem orders |
| `mexc financial-openapi fixed member-profit-record-page` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/profit_record/page` | Query paginated profit records |
| `mexc financial-openapi fixed member-pledge-currency` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/pledge_orders/currency` | Query pledge currencies |
| `mexc financial-openapi fixed member-redeem-currency` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/redeem_orders/currency` | Query redeem currencies |
| `mexc financial-openapi fixed member-profit-currency` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/profit_record/currency` | Query profit currencies |
| `mexc financial-openapi fixed member-position-detail` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/position_detail` | Query position detail |
| `mexc financial-openapi fixed member-profit-record-summary` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/profit_record_summary` | Query profit summary |
| `mexc financial-openapi fixed member-interest-detail` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/interest_detail` | Query interest detail |
| `mexc financial-openapi fixed member-redeemable-list` | `financial-openapi.fixed` | read | yes | yes | yes | `GET /api/v3/financial/cli/fixed/member/redeemable/list` | Query redeemable products |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| detail / redeem-detail / member-pledge-orders-page / member-redeem-orders-page / member-profit-record-page / member-pledge-currency / member-redeem-currency / member-profit-currency / member-position-detail / member-profit-record-summary / member-interest-detail / member-redeemable-list | `financial-openapi.fixed` | read |

## Usage Examples

```bash
# Query product detail
mexc financial-openapi fixed detail --financialId "FIN001"

# Query redeem detail
mexc financial-openapi fixed redeem-detail --financialNo "FIN001" --orderNo "ORDER001"

# Query paginated pledge orders
mexc financial-openapi fixed member-pledge-orders-page --page 1 --pageSize 20 --currencyId "8c9d9272b7834a3db22ee5b549d42588" --startTime 1783900800000 --endTime 1786579199000

# Query position detail
mexc financial-openapi fixed member-position-detail --financialNo "FIN001" --page 1 --pageSize 20

# Query interest detail
mexc financial-openapi fixed member-interest-detail --profitRecordNo "REC001" --page 1 --pageSize 20

# Query redeemable products
mexc financial-openapi fixed member-redeemable-list
```

## Workflows

### Investigate a fixed-term holding
> User: "Show me the details and redemption information for a fixed-term holding."
1. `mexc financial-openapi fixed member-pledge-orders-page --startTime <startTime> --endTime <endTime> --page 1 --pageSize 20` → locate the relevant pledge order and note its `financialNo` and `orderId`.
2. `mexc financial-openapi fixed detail --financialId <financialId>` → inspect the product's term, APR, and limits.
3. `mexc financial-openapi fixed redeem-detail --financialNo <financialNo> --orderNo <orderNo>` → inspect redemption details for the selected order.
4. `mexc financial-openapi fixed member-redeemable-list` → review the member's currently redeemable products before taking any action outside this read-only skill.

### Filter fixed-term orders by currency
> User: "Show my fixed pledge orders for USDT."
1. `mexc financial-openapi fixed member-pledge-currency` (or `member-redeem-currency` / `member-profit-currency` depending on order type) → retrieve available currencies for the filter dropdown.
2. `mexc financial-openapi fixed member-pledge-orders-page --currencyId <currencyId from step 1> --startTime <13-digit UTC Unix milliseconds> --endTime <13-digit UTC Unix milliseconds> --page 1 --pageSize 20` → query paginated orders filtered by the selected currency and time range.

## Parameter Validation Rules

### detail
- `financialId` (**required**): Product ID

### redeem-detail
- `financialNo` (**required**): Product number
- `orderNo` (**required**): Order number

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
- `financialNo` (optional): Product number
- `currencyId` (optional): Currency ID
- `startTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### member-position-detail
- `financialNo` (**required**): Product number (string, not null)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### member-interest-detail
- `profitRecordNo` (**required**): Profit record number (string, not null)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

## Notes for Agents

**General rules**
- All operations are read-only and idempotent
- memberId is extracted from request header, never passed as parameter
- All list queries are paginated

**Skill-specific notes**
- Fixed products have maturity dates; early redemption may have restrictions
- Position detail shows holdings breakdown per product
- Interest detail shows interest accrual breakdown per profit record
- Redeemable list shows products available for redemption
- Currency dropdown endpoints provide available currencies for filtering
