---
name: mexc-financial-openapi-contract-hold
scope: financial-openapi.contract-hold
signed: yes
identity: yes
visibility: public
description: 'Contract-hold financial operations via openapi gateway — query products, member assets, positions, profit records, register config, and profit calculator. Triggers: "contract hold financial products","contract hold member asset","contract hold profit record","contract hold register","contract hold profit calculator".'
---

> Auth: this subdomain requires an openapi identity (api-key auth). The account is resolved from the request's own headers, never from a parameter. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

# financial-openapi · contract-hold

Contract-hold earning financial operations exposed to clients through the openapi gateway under `/api/v3/financial/cli/contract_hold`.

## Core Concepts

Contract-hold earning allows users to earn interest on their contract positions. This subdomain provides product discovery, position tracking, profit records, and auto-registration management.

**Key entities:**
- **Product**: Contract-hold financial product with APR (base + subsidy)
- **Member Asset**: User's total earnings (total profit, yesterday's profit)
- **Position**: User's contract-hold positions by project
- **Profit Record**: Earnings distribution records
- **Register Config**: Auto-registration switch state
- **Profit Calculator**: Estimate earnings based on principal and days

**API Characteristics:**
- **Product queries**: List products, max APR, max base APR
- **Asset queries**: Member total asset summary
- **Position queries**: Member positions by project
- **Profit records**: Paginated earnings records with time filter
- **Register management**: Toggle auto-registration, query state
- **Profit calculator**: Calculate estimated returns

## Important Notes

- Most operations are **read-only** except `register` (write)
- memberId is **extracted from header** (not passed as parameter)
- `register` operation is **non-idempotent** and requires confirmation
- Profit calculator does not require a member ID, but it still requires OpenAPI authentication
- This is a public OpenAPI subdomain with openapi identity

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc financial-openapi contract-hold products-list` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/products/list` | Query product list |
| `mexc financial-openapi contract-hold products-max-apr` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/products/max_apr` | Query max APR |
| `mexc financial-openapi contract-hold products-max-base-apr` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/products/max_base_apr` | Query max base APR |
| `mexc financial-openapi contract-hold member-asset` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/member/asset` | Query member asset summary |
| `mexc financial-openapi contract-hold register` | `financial-openapi.contract-hold` | write | yes | yes | no | `POST /api/v3/financial/cli/contract_hold/register` | Modify auto-registration switch |
| `mexc financial-openapi contract-hold register-query` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/register/query` | Query registration state |
| `mexc financial-openapi contract-hold member-profit-record` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/member/profit_record` | Query paginated profit records |
| `mexc financial-openapi contract-hold member-profit-currency` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/member/profit_record/currency` | Query profit record currencies |
| `mexc financial-openapi contract-hold member-positions` | `financial-openapi.contract-hold` | read | yes | yes | yes | `GET /api/v3/financial/cli/contract_hold/member/positions` | Query member positions |
| `mexc financial-openapi contract-hold profit-calculator` | `financial-openapi.contract-hold` | read | yes | yes | yes | `POST /api/v3/financial/cli/contract_hold/profit_calculator/calculate` | Calculate estimated profit |

## Calculator Constraints

- `financialNo` must not be blank.
- `investDays` is required and must be from 0 through 1100.
- Non-positive `principalQuantity` produces a zero result; it is not rejected by the backend.
- Negative `positionQuantity` is treated as zero by the backend.

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| register | `financial-openapi.contract-hold` | write |
| products-list / products-max-apr / products-max-base-apr / member-asset / register-query / member-profit-record / member-profit-currency / member-positions / profit-calculator | `financial-openapi.contract-hold` | read |

## Usage Examples

```bash
# Query product list
mexc financial-openapi contract-hold products-list

# Query max APR
mexc financial-openapi contract-hold products-max-apr

# Query member asset summary
mexc financial-openapi contract-hold member-asset

# Enable auto-registration
mexc financial-openapi contract-hold register --data '{"switchFlag":true}'

# Query registration state
mexc financial-openapi contract-hold register-query

# Query profit records (with filters)
mexc financial-openapi contract-hold member-profit-record --page 1 --pageSize 20 --currencyId "8c9d9272b7834a3db22ee5b549d42588" --startTime 1783900800000 --endTime 1786579199000

# Calculate estimated profit
mexc financial-openapi contract-hold profit-calculator --data '{
  "financialNo": "FIN001",
  "principalQuantity": 1000.00,
  "investDays": 30
}'
```

## Workflows

### Toggle auto-registration and confirm state
> User: "Enable contract-hold auto-registration for my account."
1. `mexc financial-openapi contract-hold register-query` → read the current registration switch state.
2. `mexc financial-openapi contract-hold register --data '{"switchFlag":true}'`
   ↓ step 2 is a write: show the user a summary and wait for explicit confirmation before running.
3. `mexc financial-openapi contract-hold register-query` → re-query to confirm the switch is now enabled.

### Filter profit records by currency
> User: "Show my contract-hold earnings for USDT in the last 30 days."
1. `mexc financial-openapi contract-hold member-profit-currency` → retrieve available currencies for the filter dropdown.
2. `mexc financial-openapi contract-hold member-profit-record --currencyId <currencyId from step 1> --startTime <startTime> --endTime <endTime> --page 1 --pageSize 20` → query paginated profit records filtered by the selected currency and time range.

### Estimate profit before investing
> User: "How much would I earn on product FIN001 with 1000 USDT for 30 days?"
1. `mexc financial-openapi contract-hold products-list` → browse available products; note the target `financialNo`.
2. `mexc financial-openapi contract-hold profit-calculator --data '{"financialNo":"<financialNo from step 1>","principalQuantity":1000,"investDays":30}'` → calculate estimated returns. `financialNo` must not be blank; `investDays` must be 0–1100.

## Parameter Validation Rules

### register
- `switchFlag` (**required**): Auto-registration switch, `true` = enable, `false` = disable

### member-profit-record
- `currencyId` (optional): Profit currency ID
- `startTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `endTime` (optional): UTC Unix timestamp in milliseconds (exactly 13 digits)
- `page` (optional): Page number, default 1
- `pageSize` (optional): Page size, default 20

### profit-calculator
- `financialNo` (**required**): Target number (string, not blank)
- `principalQuantity` (**required**): Principal amount (decimal, required); returns 0 when <= 0
- `positionQuantity` (optional): Position value (decimal); negative values are treated as 0
- `investDays` (**required**): Investment days (integer, required, 0 through 1100)

## Notes for Agents

**General rules**
- Most operations are read-only; only `register` is a write
- memberId is extracted from request header, never passed as parameter
- Profit calculator does not require a member ID, but it still requires OpenAPI authentication
- `register` is non-idempotent and should not be auto-retried


**Skill-specific notes**
- Contract-hold products have both base APR and subsidy APR
- Auto-registration allows automatic enrollment in new products
- Profit records can be filtered by currency and time range
- Profit calculator helps users estimate returns before investing
- Max APR and max base APR are separate endpoints for display purposes
