// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {HeimdallGuard} from "../src/HeimdallGuard.sol";
import {HeimdallGuardFactory} from "../src/HeimdallGuardFactory.sol";
import {MockERC20, MockLendingVault, MockAavePool} from "./mocks/Mocks.sol";

/// Minimal unit tests (specs T1): funds only go to owner, access control, pause/disable,
/// partial exits.
contract HeimdallGuardTest is Test {
    HeimdallGuardFactory factory;
    MockERC20 usdc;
    MockLendingVault vault;
    MockAavePool pool;
    address aToken;
    HeimdallGuard guard;

    address admin = makeAddr("admin");
    address keeper = makeAddr("keeper");
    address ada = makeAddr("ada");
    address mallory = makeAddr("mallory");
    address borrower = makeAddr("borrower");

    bytes32 constant REASON = keccak256("S1 fast outflow");
    HeimdallGuard.PositionType constant V = HeimdallGuard.PositionType.ERC4626;
    HeimdallGuard.PositionType constant A = HeimdallGuard.PositionType.AAVE_V3;

    function setUp() public {
        usdc = new MockERC20("USD Coin", "USDC", 6);
        vault = new MockLendingVault(IERC20(address(usdc)));
        pool = new MockAavePool();
        aToken = pool.addReserve(address(usdc));
        factory = new HeimdallGuardFactory(address(pool), admin, keeper);

        usdc.mint(ada, 20_000e6);
        vm.startPrank(ada);
        usdc.approve(address(vault), type(uint256).max);
        vault.deposit(10_000e6, ada);
        usdc.approve(address(pool), type(uint256).max);
        pool.supply(address(usdc), 5_000e6, ada, 0);

        guard = HeimdallGuard(factory.createGuard());
        IERC20(address(vault)).approve(address(guard), type(uint256).max);
        guard.deposit(V, address(vault), vault.balanceOf(ada));
        IERC20(aToken).approve(address(guard), type(uint256).max);
        guard.deposit(A, address(usdc), 5_000e6);
        vm.stopPrank();
    }

    function test_factorySetup() public view {
        assertEq(guard.owner(), ada);
        assertEq(guard.factory(), address(factory));
        assertEq(factory.guardOf(ada), address(guard));
        assertEq(factory.predictGuard(ada), address(guard));
        assertTrue(factory.isGuard(address(guard)));
    }

    function test_cannotReinitializeOrCreateTwice() public {
        vm.expectRevert(HeimdallGuard.NotFactory.selector);
        guard.initialize(mallory, address(pool));
        vm.prank(address(factory));
        vm.expectRevert(HeimdallGuard.AlreadyInitialized.selector);
        guard.initialize(mallory, address(pool));
        vm.prank(ada);
        vm.expectRevert(HeimdallGuardFactory.GuardExists.selector);
        factory.createGuard();
    }

    function test_keeperExitSendsFundsOnlyToOwner() public {
        uint256 before = usdc.balanceOf(ada);
        vm.prank(keeper);
        uint256 out = guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(out, 10_000e6);
        assertEq(usdc.balanceOf(ada) - before, 10_000e6);
        assertEq(usdc.balanceOf(keeper), 0);
        assertEq(usdc.balanceOf(address(guard)), 0);

        vm.prank(keeper);
        out = guard.exit(A, address(usdc), type(uint256).max, REASON);
        assertEq(out, 5_000e6);
        assertEq(usdc.balanceOf(ada) - before, 15_000e6);
        assertEq(IERC20(aToken).balanceOf(address(guard)), 0);
    }

    function test_strangerCannotExitWithdrawPauseOrToggle() public {
        vm.startPrank(mallory);
        vm.expectRevert(HeimdallGuard.NotAuthorized.selector);
        guard.exit(V, address(vault), type(uint256).max, REASON);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.withdraw(V, address(vault), 1);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.setPaused(true);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.setKeeperEnabled(false);
        vm.expectRevert(HeimdallGuardFactory.NotKeeper.selector);
        factory.keeperExitBatch(new HeimdallGuardFactory.ExitRequest[](0));
        vm.expectRevert(HeimdallGuardFactory.NotAdmin.selector);
        factory.setKeeper(mallory);
        vm.stopPrank();
    }

    function test_keeperCannotWithdrawPauseOrToggle() public {
        vm.startPrank(keeper);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.withdraw(V, address(vault), 1);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.setPaused(false);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.setKeeperEnabled(true);
        vm.stopPrank();
    }

    function test_pauseAndDisableBlockKeeperImmediately() public {
        vm.prank(ada);
        guard.setPaused(true);
        vm.prank(keeper);
        vm.expectRevert(HeimdallGuard.NotAuthorized.selector);
        guard.exit(V, address(vault), type(uint256).max, REASON);

        vm.startPrank(ada);
        guard.setPaused(false);
        guard.setKeeperEnabled(false);
        vm.stopPrank();
        vm.prank(keeper);
        vm.expectRevert(HeimdallGuard.NotAuthorized.selector);
        guard.exit(V, address(vault), type(uint256).max, REASON);

        // Batch path is blocked too, without reverting the batch.
        HeimdallGuardFactory.ExitRequest[] memory reqs = new HeimdallGuardFactory.ExitRequest[](1);
        reqs[0] = HeimdallGuardFactory.ExitRequest(address(guard), V, address(vault), type(uint256).max, REASON);
        vm.prank(keeper);
        factory.keeperExitBatch(reqs);
        assertGt(vault.balanceOf(address(guard)), 0);

        // Owner can still exit manually.
        vm.prank(ada);
        guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(vault.balanceOf(address(guard)), 0);
    }

    function test_ownerWithdrawReturnsPositionTokens() public {
        uint256 shares = vault.balanceOf(address(guard));
        vm.startPrank(ada);
        guard.withdraw(V, address(vault), shares);
        guard.withdraw(A, address(usdc), 5_000e6);
        vm.stopPrank();
        assertEq(vault.balanceOf(ada), shares);
        assertEq(IERC20(aToken).balanceOf(ada), 5_000e6);
    }

    function test_partialExit4626RedeemsAvailableThenRest() public {
        vault.setLiquidity(6_200e6);
        uint256 before = usdc.balanceOf(ada);
        vm.prank(keeper);
        uint256 out = guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(out, 6_200e6);
        assertEq(usdc.balanceOf(ada) - before, 6_200e6);
        assertEq(vault.convertToAssets(vault.balanceOf(address(guard))), 3_800e6);

        // No liquidity: deferred, not reverted.
        vault.setLiquidity(0);
        vm.prank(keeper);
        out = guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(out, 0);

        vault.setLiquidity(type(uint256).max);
        vm.prank(keeper);
        out = guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(out, 3_800e6);
        assertEq(vault.balanceOf(address(guard)), 0);
    }

    function test_partialExitAaveRedeemsAvailableCash() public {
        // Borrowers take most of the reserve cash (5,000 supplied by ada).
        vm.prank(address(pool));
        pool.borrowOut(address(usdc), 4_000e6, borrower);
        uint256 before = usdc.balanceOf(ada);
        vm.prank(keeper);
        uint256 out = guard.exit(A, address(usdc), type(uint256).max, REASON);
        assertEq(out, 1_000e6);
        assertEq(usdc.balanceOf(ada) - before, 1_000e6);
        assertEq(IERC20(aToken).balanceOf(address(guard)), 4_000e6);

        vm.prank(keeper);
        out = guard.exit(A, address(usdc), type(uint256).max, REASON);
        assertEq(out, 0);
    }

    function test_batchContinuesPastFailures() public {
        address bob = makeAddr("bob");
        vm.prank(bob);
        HeimdallGuard bobGuard = HeimdallGuard(factory.createGuard());
        vm.prank(bob);
        bobGuard.setPaused(true);

        HeimdallGuardFactory.ExitRequest[] memory reqs = new HeimdallGuardFactory.ExitRequest[](3);
        reqs[0] = HeimdallGuardFactory.ExitRequest(address(bobGuard), V, address(vault), type(uint256).max, REASON);
        reqs[1] = HeimdallGuardFactory.ExitRequest(mallory, V, address(vault), type(uint256).max, REASON);
        reqs[2] = HeimdallGuardFactory.ExitRequest(address(guard), V, address(vault), type(uint256).max, REASON);
        uint256 before = usdc.balanceOf(ada);
        vm.prank(keeper);
        factory.keeperExitBatch(reqs);
        assertEq(usdc.balanceOf(ada) - before, 10_000e6);
        assertEq(usdc.balanceOf(mallory), 0);
    }

    function test_adminRotatesKeeperOnly() public {
        address k2 = makeAddr("k2");
        vm.prank(admin);
        factory.setKeeper(k2);
        vm.prank(keeper);
        vm.expectRevert(HeimdallGuard.NotAuthorized.selector);
        guard.exit(V, address(vault), type(uint256).max, REASON);
        // Admin has no power over guard funds.
        vm.startPrank(admin);
        vm.expectRevert(HeimdallGuard.NotAuthorized.selector);
        guard.exit(V, address(vault), type(uint256).max, REASON);
        vm.expectRevert(HeimdallGuard.NotOwner.selector);
        guard.withdraw(V, address(vault), 1);
        vm.stopPrank();
        vm.prank(k2);
        guard.exit(V, address(vault), type(uint256).max, REASON);
        assertEq(usdc.balanceOf(k2), 0);
    }
}
