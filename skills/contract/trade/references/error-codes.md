# Contract API Error Codes (Trade)

Business error codes returned in the response envelope's `code` field (HTTP stays 200; `success=false` and `code!=0` signal a business error). English text is sourced verbatim from the platform i18n bundle (`en-US.json`); `{placeholders}` are filled in at runtime.

> Scope: this list is curated to the codes these `contract trade (orders, positions, plan/stop/track orders)` interfaces can realistically return. Codes belonging to unrelated products are intentionally excluded: grid trading bots, copy trading, US-stock / RealStocks (Longbridge), Spot & Earn margin assets, marketing/activities & tournaments, and Event (prediction) futures. Codes without an English entry in the i18n bundle are omitted.


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
| 703 | `REQUIRE_TRADE_READ_PERMISSION` | Trading information read access is required |
| 704 | `REQUIRE_TRADE_WRITE_PERMISSION` | Please enable API Key trading information write access |


## Account & Contract

| Code | Constant | Meaning |
|------|----------|---------|
| 1000 | `ACCOUNT_NOT_EXISTS` | Account does not exist |
| 1001 | `CONTRACT_NOT_EXIST` | Contract does not exist |
| 1002 | `CONTRACT_NOT_ENABLE` | Contract not activated |
| 1003 | `RISK_LIMIT_LEVEL_ERROR` | Risk limit level error, please try again after adjustment |
| 1004 | `AMOUNT_ERROR` | Amount error |
| 1005 | `API_KEY_NOT_EXISTS` | API Key does not exist _(no i18n entry; provisional)_ |
| 4001 | `NOT_SUPPORT_CURRENCY` | Unsupported cryptocurrency |


## Order Placement & Cancellation

| Code | Constant | Meaning |
|------|----------|---------|
| 2001 | `ORDER_SIDE_ERROR` | Order direction error |
| 2002 | `OPEN_TYPE_ERROR` | Position type error |
| 2003 | `BID_PRICE_TOO_HIGH` | Exceeded the maximum buying price {amount}{currency}, please fill in again |
| 2004 | `ASK_PRICE_TOW_LOW` | Selling price is lower than the minimum price {amount}{currency}, please fill in again |
| 2005 | `INSUFFICIENT_BALANCE` | Balance insufficient |
| 2006 | `LEVERAGE_ERROR` | Leverage multiplier must be within the upper limit {max} and lower limit {min} |
| 2007 | `ORDER_PRICE_ERROR` | Order price error |
| 2008 | `CLOSE_VOL_INSUFFICIENT` | Insufficient number of positions can be closed |
| 2009 | `POSITION_NOT_EXISTS` | Position is nonexistent or closed |
| 2011 | `ORDER_VOL_ERROR` | Order quantity error |
| 2013 | `EXCEED_BATCH_CANCEL_MAX_SIZE` | Number of cancelled orders exceeds {count} |
| 2014 | `EXCEED_BATCH_ORDER_MAX_SIZE` | Orders in batches shall not be greater than {count} |
| 2015 | `PRICE_VOL_DECIMAL_ERROR` | Price or quantity precision error, please enter again |
| 2016 | `PLAN_ORDER_MAX_ENTRUST_OVERSIZE` | Trigger order exceeds the max. number of {count} |
| 2028 | `EXCEED_MAX_ORDER_ENTRUST_NUM` | Maximum order quantity of {count} exceeded |
| 2029 | `ORDER_TYPE_ERROR` | Order type error |
| 2030 | `EXTERNAL_OID_TOO_LONG` | External order ID too long (max. 32 digit) |
| 2034 | `EXCEED_BATCH_QUERY_MAX_SIZE` | Batch query quantity exceeds the limit of {count} |
| 2035 | `MARKET_ORDER_LEVEL_ERROR` | Market order level not supported |
| 2036 | `OPEN_POSITION_COUNT_EXCEED_MAX_NUM` | You've reached today's order limit |
| 2037 | `OPEN_POSITION_FREQUENTLY` | Trading too frequent, please try again later |
| 2038 | `EXCEED_MAX_HOLD_VOL` | Position limit reached for your current leverage tier. See Risk Limits for the maximum position size for each tier. |
| 2039 | `UNILATERAL_POSITION_CAN_NOT_HEDGE` | Counterparty's order amount shall not be greater than that of the best bid or best ask. Please select more orders in the orderbook or reduce the order amount before placing the order |
| 2040 | `ORDER_NOT_EXISTS` | Order does not exist |
| 2041 | `ORDER_CAN_NOT_BE_CANCEL` | Current order status does not allow cancelation |
| 2042 | `DUPLICATE_ORDER_ID` | Duplicate order ID |
| 2043 | `POSITION_ORDER_NOT_MATCH` | The order does not match the position |
| 2051 | `EXCEED_ORDER_MAX_VOL` | Exceeds the maximum order amount allowed for a single order |
| 2057 | `EXCEED_ORDER_MIN_VOL` | Not meeting the minimum order amount required for a single order |
| 2069 | `HOLDING_OPEN_POSITION_UNAVAILABLE_BALANCE` | Your current Cross Margin position is showing a loss. Please close the position before claiming the reward again. |
| 2077 | `ORDER_PRICE_LESS_THAN_LONG_POSITION_LIMIT` | Long: Fill price is below the liquidation price. Use a limit order instead. |
| 2078 | `ORDER_PRICE_GREATER_THAN_SHORT_POSITION_LIMIT` | Short: Fill price exceeds the liquidation price. Use a limit order instead. |


## Order Modification

| Code | Constant | Meaning |
|------|----------|---------|
| 2052 | `ORDER_NOT_APPROVAL_CAN_NOT_MODIFY` | Partially filled orders do not support price or quantity editing |
| 2053 | `ORDER_ONE_WAY_MODE_CAN_NOT_MODIFY` | One-way mode does not support limit order editing currently |
| 2054 | `ORDER_NOT_CONFIRM_CANCELLED_NOT_MODIFY` | Unable to place a new order as the system has not found any successfully canceled orders. Please try editing the order again later or place a new order. |
| 2055 | `SYS_NOT_ENABLED_CAN_NOT_MODIFY` | System does not support limit order editing currently |
| 2056 | `POSITION_REVERSE_CLOSE_TIME_OUT` | Market close order is in progress due to market volatility. Please try again later. We apologize for any inconvenience caused. |


## Leverage / Risk Limit / Margin

| Code | Constant | Meaning |
|------|----------|---------|
| 2018 | `EXCEED_MAX_DEDUCTIBLE_MARGIN` | Cannot exceed the maximum reducible margin {vol} |
| 2019 | `EXISTS_OPEN_ORDERS` | Leverage adjustment unavailable while orders are open |
| 2021 | `LEVERAGE_DIFFERENT` | Order leverage is inconsistent with the existing position leverage |
| 2022 | `POSITION_TYPE_ERROR` | Position type error |
| 2023 | `EXISTS_POSITION_MAXIMUM_LEVERAGE` | Risk limit cannot be modified at the moment due to leverage of existing open positions |
| 2024 | `EXISTS_ORDER_MAXIMUM_LEVERAGE` | There's order with leverage greater than the maximum leverage of the new order, so modifying the risk limit is unavailable at the moment |
| 2025 | `EXCEED_RISK_LIMIT_VOL` | Your current position amount is greater than the allowable amount of the new order. Please modify the risk limit and place the order again |
| 2026 | `CROSS_MARGIN_CAN_NOT_CHANGE_MARGIN` | Leverage adjustment is unavailable for cross margin |
| 2027 | `OPEN_TYPE_DIFF` | Cross and isolated position of the same direction are alternative. |
| 2031 | `EXCEED_RISK_LEVEL_MAX_NUM` | Current risk limit and maximum allowable number of positions exceeded, please adjust the risk limit |
| 2044 | `INSUFFICIENT_BALANCE_CAUSED_BY_DECREASE_LEVERAGE` | Insufficient margin to adjust leverage |
| 2045 | `MINIMUM_RATIO_ALLOWED` | Minimum ratio allowed is {number} |
| 2046 | `MAXIMUM_RATIO_ALLOWED` | Maximum ratio allowed is {number} |
| 2047 | `MINIMUM_VARIANCE_ALLOWED` | Minimum variance allowed is {number} |
| 2048 | `MAXIMUM_VARIANCE_ALLOWED` | Maximum variance allowed is {number} |
| 2049 | `LIQUIDATE_BY_ORDER_PRICE` | Liquidation may occur after the order is filled at the specified price. Please update the price before placing the order. |
| 8815 | `EXCEED_MAX_VOLUME_OF_TARGET_LEVERAGE` | Current positions exceeded the maximum number of positions {amount}{currency} that can be held at the target leverage |
| 8816 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE` | Exceeded the maximum number of positions {amount}{currency} that can be held at this leverage |
| 8817 | `NOT_SUPPORT_CHANGE_RISK_LEVEL` | Risk limiting mechanism has been upgraded. Please check the website for more information. |
| 8818 | `EXCEED_MAX_VOLUME_OF_TARGET_LEVERAGE_COST` | Current positions exceeded the maximum number of contracts {amount} that can be held at the target leverage |
| 8819 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE_COST` | Exceeded the maximum number of contracts {amount} that can be held at this leverage |
| 8820 | `EXCEED_MAX_VOLUME_OF_RISK_LIMIT_GROUP` | Due to risk control measures, your account has reached the maximum position limit and cannot open additional positions |
| 8821 | `EXCEED_MAX_VOLUME_OF_LEVEL` | The current position quantity exceeds the maximum position quantity allowed for the target leverage. The current maximum leverage that can be set is {leverage}. |
| 8822 | `EXCEED_MAX_VOLUME_OF_CURRENT_LEVERAGE_COST_POPUP` | Exceeded the maximum number of contracts {amount} that can be held at this leverage |
| 8823 | `CONTRACT_DELIVERY_NOT_ALLOW_OPEN` | This Futures pair will be delisted. New positions cannot be opened. Existing positions remain unaffected. Please close them positions before settlement to avoid potential losses. |
| 8915 | `EXISTS_OPEN_ORDERS_OR_POSITIONS_CAN_NOT_CHANGE_LEVERAGE_MODE` | Leverage mode cannot be changed as there is an active order or position. Please cancel the order or close the position first. |
| 8919 | `OVER_MAX_AMOUNT_LIMIT` | Exceeds the maximum value limit for position and open order values across all trading pairs for a single user |
| 8931 | `COUNTRY_LEVERAGE_ERROR` | Your leverage exceeds the maximum limit of {max}x in your country/region. Please adjust your leverage and try again. |


## Chase Order

| Code | Constant | Meaning |
|------|----------|---------|
| 2058 | `CHASE_ORDER_SYS_NOT_ENABLED` | Chase Order Failed: The system does not currently support chase orders |
| 2059 | `CHASE_ORDER_STATE_FINISHED` | Chase Order Failed: Your order has been filled or canceled |
| 2060 | `CHASE_ORDER_BID_STOP_PROFIT_PRICE_LOW` | Chase Order Failed: TP price must be higher than the best bid price |
| 2061 | `CHASE_ORDER_BID_STOP_LOSS_PRICE_HIGH` | Chase Order Failed: SL price must be lower than the best bid price |
| 2062 | `CHASE_ORDER_ASK_STOP_PROFIT_PRICE_HIGH` | Chase Order Failed: TP price must be lower than the best ask price |
| 2063 | `CHASE_ORDER_ASK_STOP_LOSS_PRICE_LOW` | Chase Order Failed: SL price must be higher than the best ask price |
| 2064 | `CHASE_ORDER_ONE_WAY_MODE_DISABLE` | Unable to place chase orders in one-way position mode |
| 2065 | `CHASE_ORDER_INSUFFICIENT_ASSETS` | Chase Order Failed: Insufficient assets |
| 2066 | `CHASE_ORDER_LIQUIDATE` | Chase Order Failed: This action will result in liquidation |


## Chase Limit Order

| Code | Constant | Meaning |
|------|----------|---------|
| 2070 | `CHASE_LIMIT_ORDER_DISTANCE_VALUE_EXCEED` | Price distance cannot exceed 5% of the current bid1/ask1 |
| 2071 | `CHASE_LIMIT_ORDER_MAX_DISTANCE_VALUE_EXCEED` | Max. chase distance cannot exceed 10% |
| 2072 | `CHASE_LIMIT_ORDER_USER_MAX_COUNT_EXCEED` | You can have up to {count} active chase limit orders per account at a time |
| 2073 | `CHASE_LIMIT_ORDER_USER_EXISTED_CONTRACT` | A chase limit order already exists for this Futures pair |
| 2074 | `CHASE_LIMIT_ORDER_NOT_SUPPORT_EDIT` | Chase limit orders cannot be edited |
| 2075 | `NOT_CHASE_LIMIT_ORDER` | This is not a chase limit order |
| 2076 | `CHASE_LIMIT_ORDER` | This order is a chase limit order |


## Loss-Coverage Coupon

| Code | Constant | Meaning |
|------|----------|---------|
| 2079 | `COUPON_NOT_EXIST` | Loss Coverage does not exist |
| 2080 | `COUPON_USED` | Loss Coverage used |
| 2081 | `COUPON_EXPIRED` | Loss Coverage expired |
| 2082 | `COUPON_UNAVAILABLE` | Loss Coverage unavailable |


## Plan / Trigger Order

| Code | Constant | Meaning |
|------|----------|---------|
| 3001 | `TRIGGER_PRICE_TYPE_ERROR` | Trigger order price type error |
| 3002 | `TRIGGER_TYPE_ERROR` | Trigger order type error |
| 3003 | `TRIGGER_EXECUTE_CYCLE_ERROR` | Execution cycle error |
| 3004 | `TRIGGER_PRICE_ERROR` | Trigger price error |
| 3006 | `PLAN_ORDER_STATUS_ERROR_CHANGE_PRICE_FAIL` | Order prices can only be edited for orders that have not been triggered yet |
| 3007 | `PLAN_ORDER_LONG_PROFIT_SHOULD_HIGHER_MARKET` | Take-profit price of a trigger-market long order should be higher than the trigger price |
| 3008 | `PLAN_ORDER_LONG_PROFIT_SHOULD_HIGHER` | Take-profit price of a trigger-limit long order should be higher than the order price |
| 3009 | `PLAN_ORDER_LONG_LOSS_SHOULD_LOWER_MARKET` | Stop-loss price of a trigger-market long order should be lower than the trigger price |
| 3010 | `PLAN_ORDER_LONG_LOSS_SHOULD_LOWER` | Stop-loss price of a trigger-limit long order should be lower than the order price |
| 3011 | `PLAN_ORDER_SHORT_PROFIT_SHOULD_LOWER_MARKET` | Take-profit price of a trigger-market short order should be lower than the trigger price |
| 3012 | `PLAN_ORDER_SHORT_PROFIT_SHOULD_LOWER` | Take-profit price of a trigger-limit short order should be lower than the order price |
| 3013 | `PLAN_ORDER_SHORT_LOSS_SHOULD_HIGHER_MARKET` | Stop-loss price of a short position trigger-market order should be higher than the trigger price |
| 3014 | `PLAN_ORDER_SHORT_LOSS_SHOULD_HIGHER` | Stop-loss price of a short position trigger-limit order should be higher than the order price |
| 3015 | `PLAN_ORDER_STATUS_ERROR_CHANGE_STOP_ORDER_FAIL` | Pre-set TP/SL prices can only be edited for orders that have not been triggered yet |
| 3016 | `ORDER_REQUEST_TIME_OUT` | {symbol}  order timed out |
| 3017 | `PLAN_ORDER_REQUEST_TIME_OUT` | {symbol} trigger order timed out |
| 3018 | `TRACK_ORDER_REQUEST_TIME_OUT` | {symbol}  trailing stop order timed out |
| 3019 | `CHASE_LIMIT_ORDER_REQUEST_TIME_OUT` | {symbol} chase limit order timed out |


## Take-Profit / Stop-Loss Order

| Code | Constant | Meaning |
|------|----------|---------|
| 5001 | `STOP_LOSS_AND_TAKE_PROFIT_PRICE_CAN_NOT_NULL` | Stop profit price and stop loss price cannot both be empty |
| 5002 | `STOP_LOSS_AND_TAKE_PROFIT_ORDER_NOT_EXISTS_OR_FINISHED` | Stop-limit order is nonexistent or closed |
| 5003 | `STOP_LOSS_AND_TAKE_PROFIT_PRICE_ERROR` | The price of stop-limit order error |
| 5004 | `STOP_ORDER_VOL_MORE_THAN_POSITION_VOL` | Total amount of the stop-limit order is greater than the closable amount of the position |
| 5005 | `STOP_ORDER_VOL_MORE_THAN_0` | Order failed as there is already a position TP/SL order. |
| 5006 | `REVERSE_OPEN_TYPE_DIFFERENT` | You currently have maker orders or positions in the opposite direction under a different margin mode |


## Position Mode / One-Way

| Code | Constant | Meaning |
|------|----------|---------|
| 7001 | `EXISTS_OPEN_ORDERS_CAN_NOT_CHANGE_POSITION_MODE` | Please cancel active orders or close positions before modifying position mode |
| 7002 | `POSITION_MODE_MIS_MATCH` | Position mode mismatch, please switch position mode |
| 7003 | `ONE_WAY_MODE_OPEN_TYPE_DIFF` | The new order type must be consistent with the existing order or the opening type of position |
| 7004 | `ONE_WAY_MODE_LEVERAGE_DIFFERENT` | The new order leverage must be consistent with the existing order or the leverage of the position |
| 7005 | `ONE_WAY_MODE_OPEN_FOR_CLOSE_VOL_NOT_ENOUGH` | The position amount for closing is insufficient, please check the position or the pending order |
| 7006 | `POSITION_MODE_NOT_EXIST` | Position mode does not exist _(no i18n entry; provisional)_ |
| 7008 | `NOT_MEET_MIN_ENTRUST_VALUE` | Cannot be less than the minimum order amount {value} USDT |
| 7009 | `MAX_REAL_LEVERAGE_PERCENT_POSITION_VAL_CB` | Your leverage ratio (position value/wallet balance) is too high. Please close positions, make a transfer, or deposit funds first. |


## Trading Restrictions & Contract State

| Code | Constant | Meaning |
|------|----------|---------|
| 6001 | `FORBID_TRADE` | Trading is forbidden |
| 6002 | `FORBID_OPEN_POSITION` | Position opening is forbidden. Please contact Customer Service |
| 6003 | `TIME_RANGE_ERROR` | Incorrect time range, you can select up to 90 days only |
| 6004 | `SYMBOL_IS_ABSOLUTELY` | The trading pair and status should be filled in |
| 6005 | `SYMBOL_IS_NOT_OPEN` | The trading pair is not available |
| 6006 | `APP_VERSION_NOT_SUPPORT_CROSS` | The current version does not support Cross Margin function, please use the Web version |
| 6007 | `CHANGE_OPENTYPE_NOT_SUPPORT` | Switching from Cross Margin mode to Isolated Margin Mode is not supported |
| 6008 | `APP_VERSION_BEHIND_REMIND` | Order placed successfully (The estimated liquidation price may not show accurately on the current version, kindly update to the latest version.) |
| 6009 | `FORBID_OPEN_POSITION_NEW` | Position opening is currently unavailable for your account. Please go to Help Center to submit information for risk control removal. |
| 6026 | `FORBID_OPEN_POSITION_POPUP` | Position opening is unavailable until risk control verification is completed. |
| 6027 | `FORBID_OPEN_POSITION_POPUP_BY_RISK` | Opening new positions for this pair is temporarily disabled. Only closing is allowed. |
| 6028 | `FORBID_OPEN_POSITION_NO_LONGER_ALLOWED` | After the platform's risk review, you are no longer allowed to open Futures positions. |
| 6030 | `BLOCK_CONTRACT_PAIR` | Position opening is unavailable until risk control verification is completed. |
| 8920 | `CONTRACT_FORBID_OPEN` | Due to market risk controls, only closing positions is allowed for this pair |
| 8921 | `CONTRACT_FORBID_CLOSE` | Due to market risk controls, only opening positions is allowed for this pair |
| 8922 | `CONTRACT_FORBID_OPEN_CLOSE` | Trading for this pair is suspended. Wait for trading to resume. |
| 8930 | `ONLY_MARKET_MAKER_ALLOW_TO_GET_ALL_CONTRACT_TIERS` | Not yet listed, unable to place an order |
| 8932 | `COUNTRY_BLACK_CONTRACT_ERROR` | Due to risk controls, opening new positions for this pair is temporarily disabled. Only closing is allowed. |
| 8950 | `COUNTRY_BLACK_CONTRACT_OPEN_FORBIDDEN` | For risk management reasons, opening positions for this trading pair is unavailable in your country or region. You may only close existing positions. |
| 9068 | `STOP_CONTRACT_NOT_ENABLE` | Trading unavailable due to market closure. Only order cancellations are allowed; new orders cannot be placed. |
| 9069 | `NOT_FAIR_PRICE_PROFIT_LOSS` | This trading pair only supports TP/SL triggered by fair price. |
| 9070 | `CONTRACT_ONLY_ALLOW_ISOLATED` | This pair supports Isolated Margin mode only |
| 9071 | `CONTRACT_ONLY_ALLOW_CROSS_MARGIN` | This pair supports Cross Margin mode only |
| 9082 | `OPEN_POSITION_FREQUENCY_LIMITED` | For trading security, your Futures position-opening frequency has been temporarily limited. Try again later. |
| 9083 | `LLP_LEVERAGE_ERROR` | Leverage exceeds the Low Liquidity Period (LLP) limit ({Leverage}x). Please adjust and try again. |


## Guaranteed Stop-Loss

| Code | Constant | Meaning |
|------|----------|---------|
| 9074 | `CONTRACT_ENSURE_STOP_LOSS_NOT_USED` | Guaranteed Stop-Loss is currently unavailable |
| 9075 | `CONTRACT_ENSURE_STOP_LOSS_NOT_LATEST_PRICE` | Stop-Loss trigger price can only be set to the last price |
| 9076 | `CONTRACT_ENSURE_STOP_LOSS_LONG_PRICE_INVALID` | Long SL price must be lower than current price |
| 9077 | `CONTRACT_ENSURE_STOP_LOSS_SHORT_PRICE_INVALID` | Short SL price must be higher than current price |
| 9078 | `CONTRACT_ENSURE_STOP_LOSS_LONG_COST_INVALID` | Long Guaranteed SL trigger price must be less than the entry price |
| 9079 | `CONTRACT_ENSURE_STOP_LOSS_SHORT_COST_INVALID` | Short Guaranteed SL trigger price must be higher than the entry price |
| 9080 | `CONTRACT_ENSURE_STOP_LOSS_PLAN_TRIGGER_NOT_LATEST_PRICE` | Guaranteed SL only supports last price as the trigger price |
| 9081 | `CONTRACT_ENSURE_STOP_LOSS_NO_PERMISSION` | No permission for Guaranteed SL |


## KYC / Country Compliance

| Code | Constant | Meaning |
|------|----------|---------|
| 9998 | `OPEN_POSITION_REJECT` | Only position closing is supported, as the platform does not support other operations in your current country/region |
| 11444 | `INDIA_COMPLIANCE_REJECT` | This feature is temporarily unavailable. For details, please refer to our official announcement. Thank you for your understanding and cooperation. |
| 200005 | `OPEN_POSITION_NEED_KYC` | To further enhance the security of user accounts and assets, and better protect the rights and interests of our users, please complete KYC verification before proceeding with futures trading. Thank you for your understanding and support! |
| 200006 | `OPEN_POSITION_NEED_KYC_RESTRICTED_TRADING` | Please complete KYC verification to access platform services and features and enhance your account security. |

