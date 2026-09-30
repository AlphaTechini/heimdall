// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IERC4626} from "@openzeppelin/contracts/interfaces/IERC4626.sol";
import {HeimdallGuard} from "../src/HeimdallGuard.sol";
import {HeimdallGuardFactory} from "../src/HeimdallGuardFactory.sol";
import {IAavePool} from "../src/interfaces/IAavePool.sol";

/// Fork tests against REAL Arbitrum One contracts (specs T2, N4). No mocks.
/// Skipped when ARBITRUM_ONE_RPC_URL is unset. The ERC-4626 test also needs
/// FORK_ERC4626_VAULT (a real USDC lending vault on Arbitrum One, see config/targets.arbitrum-one.json).
///
///   ARBITRUM_ONE_RPC_URL=... FORK_ERC4626_VAULT=0x... forge test --match-contract ForkExits -vv
contract ForkExitsTest is Test {
    // bgd-labs/aave-address-book src/AaveV3Arbitrum.sol: AaveV3Arbitrum.POOL
    address constant AAVE_POOL = 0x794a61358D6845594F94dc1DB02A252b5b4814aD;
    // bgd-labs/aave-address-book src/AaveV3Arbitrum.sol: AaveV3ArbitrumAssets.USDCn_UNDERLYING (native USDC)
    address constant USDC = 0xaf88d065e77c8cC2239327C5EDb3A432268e5831;

    bytes32 constant REASON = keccak256("fork test");
    address keeper = makeAddr("keeper");
    address ada = makeAddr("ada");
    HeimdallGuardFactory factory;

    function _fork() internal returns (bool) {
        string memory url = vm.envOr("ARBITRUM_ONE_RPC_URL", string(""));
        if (bytes(url).length == 0) return false;
        vm.createSelectFork(url);
        factory = new HeimdallGuardFactory(AAVE_POOL, address(this), keeper);
        return true;
    }

    function test_fork_realErc4626VaultExit() public {
        address vaultAddr = vm.envOr("FORK_ERC4626_VAULT", address(0));
        if (vaultAddr == address(0) || !_fork()) {
            vm.skip(true, "set ARBITRUM_ONE_RPC_URL and FORK_ERC4626_VAULT");
            return;
        }
        IERC4626 vault = IERC4626(vaultAddr);
        assertEq(vault.asset(), USDC, "vault asset is not native USDC");

        deal(USDC, ada, 10_000e6);
        vm.startPrank(ada);
        IERC20(USDC).approve(vaultAddr, 10_000e6);
        uint256 shares = vault.deposit(10_000e6, ada);
        HeimdallGuard guard = HeimdallGuard(factory.createGuard());
        IERC20(vaultAddr).approve(address(guard), shares);
        guard.deposit(HeimdallGuard.PositionType.ERC4626, vaultAddr, shares);
        vm.stopPrank();

        uint256 exitable = guard.exitable(HeimdallGuard.PositionType.ERC4626, vaultAddr);
        emit log_named_uint("exitable shares", exitable);

        uint256 before = IERC20(USDC).balanceOf(ada);
        vm.prank(keeper);
        uint256 out = guard.exit(HeimdallGuard.PositionType.ERC4626, vaultAddr, type(uint256).max, REASON);
        emit log_named_uint("USDC out", out);

        assertEq(IERC20(USDC).balanceOf(ada) - before, out, "funds must go to owner");
        assertEq(IERC20(USDC).balanceOf(keeper), 0);
        assertEq(IERC20(USDC).balanceOf(address(guard)), 0);
        assertApproxEqAbs(out, 10_000e6, 2, "full exit expected with normal liquidity");
    }

    function test_fork_realAaveV3Exit() public {
        if (!_fork()) {
            vm.skip(true, "set ARBITRUM_ONE_RPC_URL");
            return;
        }
        address aToken = IAavePool(AAVE_POOL).getReserveData(USDC).aTokenAddress;
        deal(USDC, ada, 5_000e6);
        vm.startPrank(ada);
        IERC20(USDC).approve(AAVE_POOL, 5_000e6);
        IAavePool(AAVE_POOL).supply(USDC, 5_000e6, ada, 0);
        HeimdallGuard guard = HeimdallGuard(factory.createGuard());
        uint256 aBal = IERC20(aToken).balanceOf(ada);
        IERC20(aToken).approve(address(guard), aBal);
        guard.deposit(HeimdallGuard.PositionType.AAVE_V3, USDC, aBal);
        vm.stopPrank();

        uint256 before = IERC20(USDC).balanceOf(ada);
        vm.prank(keeper);
        uint256 out = guard.exit(HeimdallGuard.PositionType.AAVE_V3, USDC, type(uint256).max, REASON);
        emit log_named_uint("USDC out", out);

        assertEq(IERC20(USDC).balanceOf(ada) - before, out, "funds must go to owner");
        assertApproxEqAbs(out, 5_000e6, 2);
        assertEq(IERC20(aToken).balanceOf(address(guard)), 0);
        assertEq(IERC20(USDC).balanceOf(keeper), 0);
    }
}
