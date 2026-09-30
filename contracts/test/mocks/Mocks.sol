// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

// Test-only mocks (specs X8: mocks live in test/ only).

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ERC4626} from "@openzeppelin/contracts/token/ERC20/extensions/ERC4626.sol";
import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";

contract MockERC20 is ERC20 {
    uint8 private immutable _dec;

    constructor(string memory n, string memory s, uint8 d) ERC20(n, s) {
        _dec = d;
    }

    function decimals() public view override returns (uint8) {
        return _dec;
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function burn(address from, uint256 amount) external {
        _burn(from, amount);
    }
}

/// @dev ERC-4626 vault whose redeemable liquidity can be limited, like a lending vault whose
///      assets are lent out.
contract MockLendingVault is ERC4626 {
    uint256 public liquidity = type(uint256).max;

    constructor(IERC20 asset_) ERC4626(asset_) ERC20("Mock Vault", "mVLT") {}

    function setLiquidity(uint256 l) external {
        liquidity = l;
    }

    function maxWithdraw(address owner) public view override returns (uint256) {
        return Math.min(super.maxWithdraw(owner), liquidity);
    }

    function maxRedeem(address owner) public view override returns (uint256) {
        return Math.min(super.maxRedeem(owner), convertToShares(liquidity));
    }
}

/// @dev aToken mock: 1:1 with underlying, only the pool mints/burns.
contract MockAToken is ERC20 {
    address public immutable pool;
    address public immutable underlying;

    constructor(address pool_, address underlying_) ERC20("Mock aToken", "maTKN") {
        pool = pool_;
        underlying = underlying_;
        // The aToken holds the reserve cash; the pool moves it out on withdraw/borrow.
        IERC20(underlying_).approve(pool_, type(uint256).max);
    }

    function mint(address to, uint256 amount) external {
        require(msg.sender == pool, "only pool");
        _mint(to, amount);
    }

    function burn(address from, uint256 amount) external {
        require(msg.sender == pool, "only pool");
        _burn(from, amount);
    }
}

/// @dev Minimal Aave V3 Pool mock with the same function signatures Heimdall uses.
contract MockAavePool {
    using SafeERC20 for IERC20;

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

    mapping(address => address) public aTokenOf;

    function addReserve(address asset) external returns (address aToken) {
        aToken = address(new MockAToken(address(this), asset));
        aTokenOf[asset] = aToken;
    }

    function supply(address asset, uint256 amount, address onBehalfOf, uint16) external {
        address a = aTokenOf[asset];
        IERC20(asset).safeTransferFrom(msg.sender, a, amount);
        MockAToken(a).mint(onBehalfOf, amount);
    }

    function withdraw(address asset, uint256 amount, address to) external returns (uint256) {
        address a = aTokenOf[asset];
        if (amount == type(uint256).max) amount = IERC20(a).balanceOf(msg.sender);
        MockAToken(a).burn(msg.sender, amount);
        IERC20(asset).safeTransferFrom(a, to, amount);
        return amount;
    }

    /// @dev Simulates borrowers taking cash out of the reserve.
    function borrowOut(address asset, uint256 amount, address to) external {
        IERC20(asset).safeTransferFrom(aTokenOf[asset], to, amount);
    }

    function getReserveData(address asset) external view returns (ReserveDataLegacy memory d) {
        d.aTokenAddress = aTokenOf[asset];
    }
}
