// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Script, console} from "forge-std/Script.sol";
import {HeimdallGuardFactory} from "../src/HeimdallGuardFactory.sol";

/// Deploys HeimdallGuardFactory (which deploys the HeimdallGuard implementation).
///
/// Env:
///   PRIVATE_KEY      deployer key (never commit it)
///   KEEPER_ADDRESS   Heimdall keeper address (public address of the server's KEEPER_PRIVATE_KEY)
///   AAVE_V3_POOL     Aave V3 Pool for this chain, or unset/0 if unsupported
///                    Arbitrum One:     0x794a61358D6845594F94dc1DB02A252b5b4814aD
///                    Arbitrum Sepolia: 0xBfC91D59fdAA134A4ED45f7B584cAf96D7792Eff
///                    (source: bgd-labs/aave-address-book, see config/targets.arbitrum-one.json)
///   ADMIN_ADDRESS    optional, defaults to the deployer
///
/// Arbitrum Sepolia:
///   forge script script/Deploy.s.sol --rpc-url arbitrum_sepolia --broadcast --verify
contract Deploy is Script {
    function run() external returns (HeimdallGuardFactory factory) {
        uint256 pk = vm.envUint("PRIVATE_KEY");
        address deployer = vm.addr(pk);
        address keeper = vm.envAddress("KEEPER_ADDRESS");
        address aavePool = vm.envOr("AAVE_V3_POOL", address(0));
        address admin = vm.envOr("ADMIN_ADDRESS", deployer);

        vm.startBroadcast(pk);
        factory = new HeimdallGuardFactory(aavePool, admin, keeper);
        vm.stopBroadcast();

        console.log("chainId", block.chainid);
        console.log("HeimdallGuardFactory", address(factory));
        console.log("HeimdallGuard implementation", factory.implementation());

        string memory obj = "deployment";
        vm.serializeUint(obj, "chainId", block.chainid);
        vm.serializeAddress(obj, "factory", address(factory));
        vm.serializeAddress(obj, "implementation", factory.implementation());
        vm.serializeAddress(obj, "aavePool", aavePool);
        vm.serializeAddress(obj, "keeper", keeper);
        string memory json = vm.serializeAddress(obj, "admin", admin);
        vm.writeJson(json, string.concat("./deployments/", vm.toString(block.chainid), ".json"));
    }
}
