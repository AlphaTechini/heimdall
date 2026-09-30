// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IERC4626} from "@openzeppelin/contracts/interfaces/IERC4626.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {IAavePool} from "./interfaces/IAavePool.sol";

interface IHeimdallKeeperSource {
    function keeper() external view returns (address);
}

/// @title HeimdallGuard
/// @notice A personal, non-upgradeable guard contract (EIP-1167 clone) that holds a depositor's
///         ERC-4626 vault shares or Aave V3 aTokens. Heimdall's keeper can trigger an exit, but
///         every exit sends the redeemed assets to `owner` and nowhere else.
/// @dev Experimental, unaudited hackathon code.
contract HeimdallGuard is ReentrancyGuard {
    using SafeERC20 for IERC20;

    enum PositionType {
        ERC4626,
        AAVE_V3
    }

    /// @notice The factory that deployed the implementation. Stored as an immutable in the
    ///         implementation's code, so every clone shares it. Only it may initialize a clone,
    ///         and it is the source of the global keeper address.
    address public immutable factory;
    /// @notice The depositor. Set once in `initialize`, never changeable.
    address public owner;
    /// @notice Aave V3 Pool for this chain (zero if unsupported). Set once in `initialize`.
    address public aavePool;
    /// @notice Owner switch: when false the keeper cannot exit this guard.
    bool public keeperEnabled;
    /// @notice Owner switch: when true keeper exits are paused (owner actions still work).
    bool public paused;
    bool private _initialized;

    event Initialized(address indexed owner, address indexed factory, address aavePool);
    event Deposited(PositionType indexed positionType, address indexed target, address token, uint256 amount);
    event Withdrawn(PositionType indexed positionType, address indexed target, address token, uint256 amount);
    event Exited(
        PositionType indexed positionType,
        address indexed target,
        uint256 amountOut,
        uint256 burned,
        uint256 remaining,
        bytes32 reasonHash,
        address caller
    );
    event ExitDeferred(
        PositionType indexed positionType, address indexed target, uint256 remaining, bytes32 reasonHash, address caller
    );
    event KeeperToggled(bool enabled);
    event Paused(bool paused);

    error AlreadyInitialized();
    error NotFactory();
    error NotOwner();
    error NotAuthorized();
    error ZeroAddress();
    error AaveUnsupported();
    error ZeroAmount();

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    /// @dev Locks the implementation contract so it can never be initialized directly.
    constructor() {
        factory = msg.sender;
        _initialized = true;
    }

    /// @notice Called once by the factory right after cloning.
    function initialize(address owner_, address aavePool_) external {
        if (msg.sender != factory) revert NotFactory();
        if (_initialized) revert AlreadyInitialized();
        if (owner_ == address(0)) revert ZeroAddress();
        _initialized = true;
        owner = owner_;
        aavePool = aavePool_;
        keeperEnabled = true;
        emit Initialized(owner_, msg.sender, aavePool_);
    }

    // ------------------------------------------------------------------
    // Owner functions
    // ------------------------------------------------------------------

    /// @notice Move position tokens (vault shares or aTokens) from the owner into this guard.
    ///         The owner must approve this guard for the position token first.
    function deposit(PositionType t, address target, uint256 amount) external onlyOwner nonReentrant {
        if (amount == 0) revert ZeroAmount();
        address token = positionToken(t, target);
        IERC20(token).safeTransferFrom(msg.sender, address(this), amount);
        emit Deposited(t, target, token, amount);
    }

    /// @notice Return position tokens (not redeemed) to the owner.
    function withdraw(PositionType t, address target, uint256 amount) external onlyOwner nonReentrant {
        if (amount == 0) revert ZeroAmount();
        address token = positionToken(t, target);
        IERC20(token).safeTransfer(owner, amount);
        emit Withdrawn(t, target, token, amount);
    }

    function setKeeperEnabled(bool enabled) external onlyOwner {
        keeperEnabled = enabled;
        emit KeeperToggled(enabled);
    }

    function setPaused(bool paused_) external onlyOwner {
        paused = paused_;
        emit Paused(paused_);
    }

    // ------------------------------------------------------------------
    // Exit (owner, keeper, or the factory's keeper batch)
    // ------------------------------------------------------------------

    /// @notice Redeem as much of the position as is available right now, up to `maxAmount`
    ///         position-token units (shares for ERC-4626, aTokens for Aave V3). Pass
    ///         type(uint256).max for "everything". The receiver is always `owner`.
    ///         Never reverts because liquidity is short: emits `ExitDeferred` instead.
    /// @return amountOut Underlying assets sent to the owner.
    function exit(PositionType t, address target, uint256 maxAmount, bytes32 reasonHash)
        external
        nonReentrant
        returns (uint256 amountOut)
    {
        if (msg.sender != owner) {
            address k = IHeimdallKeeperSource(factory).keeper();
            if (msg.sender != k && msg.sender != factory) revert NotAuthorized();
            if (!keeperEnabled || paused) revert NotAuthorized();
        }
        if (t == PositionType.ERC4626) {
            return _exit4626(target, maxAmount, reasonHash);
        }
        return _exitAave(target, maxAmount, reasonHash);
    }

    // ------------------------------------------------------------------
    // Views
    // ------------------------------------------------------------------

    /// @notice The token this guard holds for a position: the vault itself (shares) for
    ///         ERC-4626, or the reserve's aToken for Aave V3 (target = underlying asset).
    function positionToken(PositionType t, address target) public view returns (address) {
        if (t == PositionType.ERC4626) return target;
        if (aavePool == address(0)) revert AaveUnsupported();
        return IAavePool(aavePool).getReserveData(target).aTokenAddress;
    }

    /// @notice Position-token balance held by this guard.
    function held(PositionType t, address target) external view returns (uint256) {
        return IERC20(positionToken(t, target)).balanceOf(address(this));
    }

    /// @notice How much could be exited right now (position-token units).
    function exitable(PositionType t, address target) external view returns (uint256) {
        if (t == PositionType.ERC4626) {
            return _available4626(target, type(uint256).max);
        }
        (uint256 amount,) = _availableAave(target, type(uint256).max);
        return amount;
    }

    // ------------------------------------------------------------------
    // Internals
    // ------------------------------------------------------------------

    function _available4626(address vault, uint256 maxAmount) internal view returns (uint256 shares) {
        shares = IERC20(vault).balanceOf(address(this));
        if (maxAmount < shares) shares = maxAmount;
        try IERC4626(vault).maxRedeem(address(this)) returns (uint256 maxR) {
            if (maxR < shares) shares = maxR;
        } catch {
            shares = 0;
        }
    }

    function _exit4626(address vault, uint256 maxAmount, bytes32 reasonHash) internal returns (uint256 assets) {
        uint256 shares = _available4626(vault, maxAmount);
        if (shares > 0) {
            // receiver is hardcoded to owner (specs C3)
            try IERC4626(vault).redeem(shares, owner, address(this)) returns (uint256 out) {
                assets = out;
            } catch {
                shares = 0;
            }
        }
        uint256 remaining = IERC20(vault).balanceOf(address(this));
        if (shares == 0) {
            emit ExitDeferred(PositionType.ERC4626, vault, remaining, reasonHash, msg.sender);
            return 0;
        }
        emit Exited(PositionType.ERC4626, vault, assets, shares, remaining, reasonHash, msg.sender);
    }

    function _availableAave(address asset, uint256 maxAmount) internal view returns (uint256 amount, address aToken) {
        aToken = positionToken(PositionType.AAVE_V3, asset);
        amount = IERC20(aToken).balanceOf(address(this));
        if (maxAmount < amount) amount = maxAmount;
        uint256 cash = IERC20(asset).balanceOf(aToken);
        if (cash < amount) amount = cash;
        // Aave V3.1+ tracks a virtual balance that bounds withdrawals; older pools lack it.
        try IAavePool(aavePool).getVirtualUnderlyingBalance(asset) returns (uint128 v) {
            if (v < amount) amount = v;
        } catch {}
    }

    function _exitAave(address asset, uint256 maxAmount, bytes32 reasonHash) internal returns (uint256 out) {
        (uint256 amount, address aToken) = _availableAave(asset, maxAmount);
        uint256 before = IERC20(aToken).balanceOf(address(this));
        if (amount > 0) {
            // receiver is hardcoded to owner (specs C3)
            // Withdrawing the whole balance uses Aave's max sentinel so no aToken dust is left.
            uint256 req = amount == before ? type(uint256).max : amount;
            try IAavePool(aavePool).withdraw(asset, req, owner) returns (uint256 w) {
                out = w;
            } catch {
                amount = 0;
            }
        }
        uint256 remaining = IERC20(aToken).balanceOf(address(this));
        if (amount == 0) {
            emit ExitDeferred(PositionType.AAVE_V3, asset, remaining, reasonHash, msg.sender);
            return 0;
        }
        emit Exited(PositionType.AAVE_V3, asset, out, before - remaining, remaining, reasonHash, msg.sender);
    }
}
