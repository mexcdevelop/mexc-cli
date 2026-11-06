# Contract API Error Codes (Account)

Business error codes returned in the response envelope's `code` field (HTTP stays 200; `success=false` and `code!=0` signal a business error). English text is sourced verbatim from the platform i18n bundle (`en-US.json`); `{placeholders}` are filled in at runtime.

> Scope: this list is curated to the codes these `contract account (assets, leverage, margin, position mode)` interfaces can realistically return. Codes belonging to unrelated products are intentionally excluded: grid trading bots, copy trading, US-stock / RealStocks (Longbridge), Spot & Earn margin assets, marketing/activities & tournaments, and Event (prediction) futures. Codes without an English entry in the i18n bundle are omitted.


## System & Request

| Code | Constant | Meaning |
|------|----------|---------|
| 500 | `INTERNAL_ERROR` | Internal error. Please try again. |
| 501 | `SYSTEM_BUSY` | System busy, try again later |
| 506 | `UNKNOWN_ORIGIN` | Unknown source of request |
| 510 | `REQUEST_FREQUENTLY` | Requests are too frequent, please try again later |
| 511 | `REQUEST_FORBIDDEN` | You do not have access to the interface |
| 513 | `REQUEST_INVALID` | Invalid request, please try again later |
| 600 | `PARAM_ERROR` | Parameter error |
| 601 | `PARSE_DATA_ERROR` | Data parsing error |
| 603 | `REPEAT_REQUEST` | Repeated request |
| 604 | `FUNCTION_UPDATING` | Sorry, this feature is under maintenance |
| 801 | `SYSTEM_MAINTENANCE` | System under maintenance |
| 9999 | `COMMON_ERROR` | Network error. Please try again. |


## Authentication & Permission

| Code | Constant | Meaning |
|------|----------|---------|
| 401 | `UNAUTHORIZED` | Not logged in or login has expired |
| 402 | `API_KEY_EXPIRE` | API Key expired, please apply again |
| 406 | `IP_NOT_WHITE_LIST` | Accessing IP is not in the whitelist |
| 602 | `VALID_SIGN_ERROR` | Confirming signature failed |
| 701 | `REQUIRE_ACCOUNT_READ_PERMISSION` | Please enable API Key read access |
| 702 | `REQUIRE_ACCOUNT_WRITE_PERMISSION` | Please enable API Key write access |
| 703 | `REQUIRE_TRADE_READ_PERMISSION` | Trading information read access is required |
| 704 | `REQUIRE_TRADE_WRITE_PERMISSION` | Please enable API Key trading information write access |


## Account & Contract

| Code | Constant | Meaning |
|------|----------|---------|
| 1000 | `ACCOUNT_NOT_EXISTS` | Account does not exist |
| 1001 | `CONTRACT_NOT_EXIST` | Contract does not exist |
| 1002 | `CONTRACT_NOT_ENABLE` | Contract not activated |
| 1004 | `AMOUNT_ERROR` | Amount error |
| 1005 | `API_KEY_NOT_EXISTS` | API Key does not exist _(no i18n entry; provisional)_ |
| 4001 | `NOT_SUPPORT_CURRENCY` | Unsupported cryptocurrency |


## Query Parameters

| Code | Constant | Meaning |
|------|----------|---------|
| 6003 | `TIME_RANGE_ERROR` | Incorrect time range, you can select up to 90 days only |
| 6004 | `SYMBOL_IS_ABSOLUTELY` | The trading pair and status should be filled in |
| 6005 | `SYMBOL_IS_NOT_OPEN` | The trading pair is not available |


## Leverage / Margin / Risk Limit

| Code | Constant | Meaning |
|------|----------|---------|
| 2006 | `LEVERAGE_ERROR` | Leverage multiplier must be within the upper limit {max} and lower limit {min} |
| 2009 | `POSITION_NOT_EXISTS` | Position is nonexistent or closed |
| 2018 | `EXCEED_MAX_DEDUCTIBLE_MARGIN` | Cannot exceed the maximum reducible margin {vol} |
| 2019 | `EXISTS_OPEN_ORDERS` | Leverage adjustment unavailable while orders are open |
| 2021 | `LEVERAGE_DIFFERENT` | Order leverage is inconsistent with the existing position leverage |
| 2023 | `EXISTS_POSITION_MAXIMUM_LEVERAGE` | Risk limit cannot be modified at the moment due to leverage of existing open positions |
| 2024 | `EXISTS_ORDER_MAXIMUM_LEVERAGE` | There's order with leverage greater than the maximum leverage of the new order, so modifying the risk limit is unavailable at the moment |
| 2025 | `EXCEED_RISK_LIMIT_VOL` | Your current position amount is greater than the allowable amount of the new order. Please modify the risk limit and place the order again |
| 2026 | `CROSS_MARGIN_CAN_NOT_CHANGE_MARGIN` | Leverage adjustment is unavailable for cross margin |
| 2027 | `OPEN_TYPE_DIFF` | Cross and isolated position of the same direction are alternative. |
| 2044 | `INSUFFICIENT_BALANCE_CAUSED_BY_DECREASE_LEVERAGE` | Insufficient margin to adjust leverage |
| 7001 | `EXISTS_OPEN_ORDERS_CAN_NOT_CHANGE_POSITION_MODE` | Please cancel active orders or close positions before modifying position mode |
| 8815 | `EXCEED_MAX_VOLUME_OF_TARGET_LEVERAGE` | Current positions exceeded the maximum number of positions {amount}{currency} that can be held at the target leverage |
| 8816 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE` | Exceeded the maximum number of positions {amount}{currency} that can be held at this leverage |
| 8817 | `NOT_SUPPORT_CHANGE_RISK_LEVEL` | Risk limiting mechanism has been upgraded. Please check the website for more information. |
| 8818 | `EXCEED_MAX_VOLUME_OF_TARGET_LEVERAGE_COST` | Current positions exceeded the maximum number of contracts {amount} that can be held at the target leverage |
| 8819 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE_COST` | Exceeded the maximum number of contracts {amount} that can be held at this leverage |
| 8820 | `EXCEED_MAX_VOLUME_OF_RISK_LIMIT_GROUP` | Due to risk control measures, your account has reached the maximum position limit and cannot open additional positions |
| 8821 | `EXCEED_MAX_VOLUME_OF_LEVEL` | The current position quantity exceeds the maximum position quantity allowed for the target leverage. The current maximum leverage that can be set is {leverage}. |
| 8822 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE_COST_POPUP` | Exceeded the maximum number of contracts {amount} that can be held at this leverage |
| 8915 | `EXISTS_OPEN_ORDERS_OR_POSITIONS_CAN_NOT_CHANGE_LEVERAGE_MODE` | Leverage mode cannot be changed as there is an active order or position. Please cancel the order or close the position first. |
| 8931 | `COUNTRY_LEVERAGE_ERROR` | Your leverage exceeds the maximum limit of {max}x in your country/region. Please adjust your leverage and try again. |


## Position Mode

| Code | Constant | Meaning |
|------|----------|---------|
| 7002 | `POSITION_MODE_MIS_MATCH` | Position mode mismatch, please switch position mode |
| 7006 | `POSITION_MODE_NOT_EXIST` | Position mode does not exist _(no i18n entry; provisional)_ |


## Multi-Asset (Unified) Margin Mode

| Code | Constant | Meaning |
|------|----------|---------|
| 6006 | `APP_VERSION_NOT_SUPPORT_CROSS` | The current version does not support Cross Margin function, please use the Web version |
| 6007 | `CHANGE_OPENTYPE_NOT_SUPPORT` | Switching from Cross Margin mode to Isolated Margin Mode is not supported |
| 6010 | `MULTI_ASSETS_CANNOT_OPEN_MODE` | Multi-asset mode isn't available for your account |
| 6011 | `MULTI_ASSETS_HAS_POSITION_ORDER_TO_MULTI` | Unable to switch to Multi-Asset mode as there are currently active orders or positions. Please cancel the orders or close the positions first. |
| 6012 | `MULTI_ASSETS_IS_SUB_ACCOUNT` | Sub-accounts cannot use Multi-Asset mode |
| 6014 | `MULTI_ASSETS_HAS_DEBT_TO_MULTI` | Your account has liabilities and cannot switch to Multi-Asset mode |
| 6015 | `MULTI_ASSETS_HAS_DEBT_TO_SINGLE` | The account has liabilities and cannot switch to Single-Asset mode. Please clear the liabilities first. |
| 6016 | `MULTI_ASSETS_HAS_POSITION_ORDER_TO_SINGLE` | Unable to switch to Single-Asset mode as there are currently active orders or positions. Please cancel the orders or close the positions first. |
| 6017 | `MULTI_ASSETS_PAY_DEBT_TIME_EXPIRE` | The current price has expired. Please request the price again. |
| 6018 | `MULTI_ASSETS_PAY_DEBT_RISK` | The current exchange rate is unfavorable, and this action may put your account at risk. Please request a new price |
| 6019 | `MULTI_ASSETS_PLACE_ORDER` | Coin-margined trading is not supported in Multi-Asset mode |
| 6020 | `MULTI_ASSETS_POSITION_OPEN_TYPE` | Only cross margin is allowed in Multi-Asset mode |
| 6021 | `MULTI_ASSETS_DEPT_CONFIG` | There is an issue with the liability crypto. Please try again later. |
| 6022 | `MULTI_ASSETS_CALL_THREE_SERVICE` | Trading error. Please try again later. |
| 6023 | `MULTI_ASSETS_IS_MARKET_MAKER` | Market maker accounts cannot use Multi-Asset mode |


## Trading Restrictions

| Code | Constant | Meaning |
|------|----------|---------|
| 6001 | `FORBID_TRADE` | Trading is forbidden |
| 6002 | `FORBID_OPEN_POSITION` | Position opening is forbidden. Please contact Customer Service |
| 6009 | `FORBID_OPEN_POSITION_NEW` | Position opening is currently unavailable for your account. Please go to Help Center to submit information for risk control removal. |

