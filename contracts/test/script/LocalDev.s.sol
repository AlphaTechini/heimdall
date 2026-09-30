// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

// LOCAL DEVELOPMENT ONLY (plain anvil, chain id 31337, no fork).
// Deploys test mocks so the server and web app can be exercised without Arbitrum RPC access.
// This is NOT the demo path: the demo runs on an Arbitrum One fork against real protocol
// contracts (specs N4, X8). Mocks stay under test/.
//
//   anvil &
//   forge script test/script/LocalDev.s.sol --rpc-url http://127.0.0.1:8545 --broadcast

import {Script, console} from "forge-std/Script.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {HeimdallGuardFactory} from "../../src/HeimdallGuardFactory.sol";
import {MockERC20, MockLendingVault, MockAavePool, MockPriceFeed} from "../mocks/Mocks.sol";

contract LocalDev is Script {
    // anvil default mnemonic accounts
    uint256 constant DEPLOYER_PK = 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80; // #0
    uint256 constant ADA_PK = 0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d; // #1
    uint256 constant BEN_PK = 0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a; // #2
    uint256 constant CROWD_PK = 0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6; // #3
    address constant KEEPER = 0xa0Ee7A142d267C1f36714E4a8F75612F20a79720; // #9

    function run() external {
        address ada = vm.addr(ADA_PK);
        address ben = vm.addr(BEN_PK);
        address crowd = vm.addr(CROWD_PK);

        vm.startBroadcast(DEPLOYER_PK);
        MockERC20 usdc = new MockERC20("USD Coin (local mock)", "USDC", 6);
        MockLendingVault vault = new MockLendingVault(IERC20(address(usdc)));
        MockAavePool pool = new MockAavePool();
        address aToken = pool.addReserve(address(usdc));
        HeimdallGuardFactory factory = new HeimdallGuardFactory(address(pool), vm.addr(DEPLOYER_PK), KEEPER);
        // Chainlink-style feeds (8 decimals): the price the markets use, an independent
        // reference, a stablecoin collateral price, and ETH/USD for tip conversion.
        // Local mock values only; the demo fork reads real feeds.
        MockPriceFeed marketFeed = new MockPriceFeed(8, 1e8);
        MockPriceFeed referenceFeed = new MockPriceFeed(8, 1e8);
        MockPriceFeed collateralFeed = new MockPriceFeed(8, 1e8);
        MockPriceFeed ethUsdFeed = new MockPriceFeed(8, 2_500e8);
        usdc.mint(ada, 15_000e6);
        usdc.mint(ben, 10_000e6);
        usdc.mint(crowd, 200_000e6);
        vm.stopBroadcast();

        vm.startBroadcast(ADA_PK);
        usdc.approve(address(vault), type(uint256).max);
        vault.deposit(10_000e6, ada);
        usdc.approve(address(pool), type(uint256).max);
        pool.supply(address(usdc), 5_000e6, ada, 0);
        vm.stopBroadcast();

        vm.startBroadcast(BEN_PK);
        usdc.approve(address(vault), type(uint256).max);
        vault.deposit(10_000e6, ben);
        vm.stopBroadcast();

        vm.startBroadcast(CROWD_PK);
        usdc.approve(address(vault), type(uint256).max);
        vault.deposit(200_000e6, crowd);
        // Borrowers use 60% of the vault, like a real lending vault.
        vault.lend(132_000e6, crowd);
        vm.stopBroadcast();

        string memory obj = "local";
        vm.serializeUint(obj, "chainId", block.chainid);
        vm.serializeAddress(obj, "factory", address(factory));
        vm.serializeAddress(obj, "usdc", address(usdc));
        vm.serializeAddress(obj, "vault", address(vault));
        vm.serializeAddress(obj, "aavePool", address(pool));
        vm.serializeAddress(obj, "aToken", aToken);
        vm.serializeAddress(obj, "marketFeed", address(marketFeed));
        vm.serializeAddress(obj, "referenceFeed", address(referenceFeed));
        vm.serializeAddress(obj, "collateralFeed", address(collateralFeed));
        vm.serializeAddress(obj, "ethUsdFeed", address(ethUsdFeed));
        vm.serializeAddress(obj, "deployer", vm.addr(DEPLOYER_PK));
        vm.serializeAddress(obj, "keeper", KEEPER);
        vm.serializeAddress(obj, "ada", ada);
        vm.serializeAddress(obj, "ben", ben);
        string memory json = vm.serializeAddress(obj, "crowd", crowd);
        vm.writeJson(json, "./deployments/31337.json");
        console.log("factory", address(factory));
        console.log("vault", address(vault));
    }
}
