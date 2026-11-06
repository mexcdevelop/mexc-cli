# Contract API Error Codes (Market)

Business error codes returned in the response envelope's `code` field (HTTP stays 200; `success=false` and `code!=0` signal a business error). English text is sourced verbatim from the platform i18n bundle (`en-US.json`); `{placeholders}` are filled in at runtime.

> Scope: this list is curated to the codes these `contract market (public market-data)` interfaces can realistically return. Codes belonging to unrelated products are intentionally excluded: grid trading bots, copy trading, US-stock / RealStocks (Longbridge), Spot & Earn margin assets, marketing/activities & tournaments, and Event (prediction) futures. Codes without an English entry in the i18n bundle are omitted.


## System & Request

| Code | Constant | Meaning |
|------|----------|---------|
| 500 | `INTERNAL_ERROR` | Internal error. Please try again. |
| 501 | `SYSTEM_BUSY` | System busy, try again later |
| 510 | `REQUEST_FREQUENTLY` | Requests are too frequent, please try again later |
| 511 | `REQUEST_FORBIDDEN` | You do not have access to the interface |
| 513 | `REQUEST_INVALID` | Invalid request, please try again later |
| 604 | `FUNCTION_UPDATING` | Sorry, this feature is under maintenance |
| 9999 | `COMMON_ERROR` | Network error. Please try again. |


## Parameters

| Code | Constant | Meaning |
|------|----------|---------|
| 600 | `PARAM_ERROR` | Parameter error |
| 601 | `PARSE_DATA_ERROR` | Data parsing error |
| 603 | `REPEAT_REQUEST` | Repeated request |


## Contract & Trading Pair

| Code | Constant | Meaning |
|------|----------|---------|
| 1001 | `CONTRACT_NOT_EXIST` | Contract does not exist |
| 1002 | `CONTRACT_NOT_ENABLE` | Contract not activated |
| 4001 | `NOT_SUPPORT_CURRENCY` | Unsupported cryptocurrency |
| 6003 | `TIME_RANGE_ERROR` | Incorrect time range, you can select up to 90 days only |
| 6004 | `SYMBOL_IS_ABSOLUTELY` | The trading pair and status should be filled in |
| 6005 | `SYMBOL_IS_NOT_OPEN` | The trading pair is not available |

