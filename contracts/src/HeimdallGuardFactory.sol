// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Clones} from "@openzeppelin/contracts/proxy/Clones.sol";
import {HeimdallGuard} from "./HeimdallGuard.sol";

/// @title HeimdallGuardFactory
/// @notice Deploys one HeimdallGuard per user as an EIP-1167 minimal-proxy clone of a single,
///         fixed implementation, and lets the global keeper exit many guards in one transaction.
/// @dev The admin can only rotate the keeper address. It has no power over guard funds, owners
///      or policies (specs C9). Experimental, unaudited hackathon code.
contract HeimdallGuardFactory {
    struct ExitRequest {
        address guard;
        HeimdallGuard.PositionType positionType;
        address target;
        uint256 maxAmount;
        bytes32 reasonHash;
    }

    /// @notice The guard implementation every clone points to. Deployed by this factory.
    address public immutable implementation;
    /// @notice Aave V3 Pool for this chain (zero if unsupported).
    address public immutable aavePool;

    address public admin;
    address public keeper;

    mapping(address owner => address guard) public guardOf;
    mapping(address guard => bool) public isGuard;

    event GuardCreated(address indexed owner, address indexed guard);
    event KeeperChanged(address indexed previousKeeper, address indexed newKeeper);
    event AdminChanged(address indexed previousAdmin, address indexed newAdmin);
    event BatchExitResult(
        uint256 indexed index, address indexed guard, address indexed target, bool success, uint256 amountOut, bytes err
    );

    error GuardExists();
    error NotAdmin();
    error NotKeeper();
    error ZeroAddress();
    error UnknownGuard(address guard);

    constructor(address aavePool_, address admin_, address keeper_) {
        if (admin_ == address(0) || keeper_ == address(0)) revert ZeroAddress();
        implementation = address(new HeimdallGuard());
        aavePool = aavePool_;
        admin = admin_;
        keeper = keeper_;
        emit AdminChanged(address(0), admin_);
        emit KeeperChanged(address(0), keeper_);
    }

    /// @notice Create the caller's guard. One guard per address; the address is deterministic.
    function createGuard() external returns (address guard) {
        if (guardOf[msg.sender] != address(0)) revert GuardExists();
        guard = Clones.cloneDeterministic(implementation, _salt(msg.sender));
        guardOf[msg.sender] = guard;
        isGuard[guard] = true;
        HeimdallGuard(guard).initialize(msg.sender, aavePool);
        emit GuardCreated(msg.sender, guard);
    }

    /// @notice The address `owner`'s guard has (or will have once created).
    function predictGuard(address owner) external view returns (address) {
        return Clones.predictDeterministicAddress(implementation, _salt(owner), address(this));
    }

    /// @notice Exit several guards in one (priority) transaction. Keeper only. A failing item
    ///         never reverts the batch; each item emits `BatchExitResult`.
    function keeperExitBatch(ExitRequest[] calldata reqs) external {
        if (msg.sender != keeper) revert NotKeeper();
        for (uint256 i = 0; i < reqs.length; i++) {
            ExitRequest calldata r = reqs[i];
            // Only guards created here can be called, so the keeper cannot reach arbitrary targets.
            if (!isGuard[r.guard]) {
                emit BatchExitResult(i, r.guard, r.target, false, 0, abi.encodeWithSelector(UnknownGuard.selector, r.guard));
                continue;
            }
            try HeimdallGuard(r.guard).exit(r.positionType, r.target, r.maxAmount, r.reasonHash) returns (uint256 out) {
                emit BatchExitResult(i, r.guard, r.target, true, out, "");
            } catch (bytes memory err) {
                emit BatchExitResult(i, r.guard, r.target, false, 0, err);
            }
        }
    }

    function setKeeper(address newKeeper) external {
        if (msg.sender != admin) revert NotAdmin();
        if (newKeeper == address(0)) revert ZeroAddress();
        emit KeeperChanged(keeper, newKeeper);
        keeper = newKeeper;
    }

    function transferAdmin(address newAdmin) external {
        if (msg.sender != admin) revert NotAdmin();
        if (newAdmin == address(0)) revert ZeroAddress();
        emit AdminChanged(admin, newAdmin);
        admin = newAdmin;
    }

    function _salt(address owner) private pure returns (bytes32) {
        return bytes32(uint256(uint160(owner)));
    }
}
