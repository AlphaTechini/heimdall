// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @notice Minimal subset of the Aave V3 Pool interface used by Heimdall.
/// @dev Copied from aave-dao/aave-v3-origin `src/contracts/interfaces/IPool.sol` and
///      `src/contracts/protocol/libraries/types/DataTypes.sol` (ReserveDataLegacy).
///      `getReserveData` returns the legacy struct on every V3 version, so the layout below
///      is stable across V3.0 to V3.x.
interface IAavePool {
    struct ReserveConfigurationMap {
        uint256 data;
    }

    struct ReserveDataLegacy {
        ReserveConfigurationMap configuration;
        uint128 liquidityIndex;
        uint128 currentLiquidityRate;
        uint128 variableBorrowIndex;
        uint128 currentVariableBorrowRate;
        uint128 currentStableBorrowRate;
        uint40 lastUpdateTimestamp;
        uint16 id;
        address aTokenAddress;
        address stableDebtTokenAddress;
        address variableDebtTokenAddress;
        address interestRateStrategyAddress;
        uint128 accruedToTreasury;
        uint128 unbacked;
        uint128 isolationModeTotalDebt;
    }

    function supply(address asset, uint256 amount, address onBehalfOf, uint16 referralCode) external;

    function withdraw(address asset, uint256 amount, address to) external returns (uint256);

    function getReserveData(address asset) external view returns (ReserveDataLegacy memory);

    /// @dev Only on Aave V3.1+ pools. Heimdall calls it through try/catch.
    function getVirtualUnderlyingBalance(address asset) external view returns (uint128);
}
