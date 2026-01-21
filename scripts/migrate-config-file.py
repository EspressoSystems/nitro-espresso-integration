#!/usr/bin/env python3
import json
import sys
from collections import OrderedDict

ESPRESSO_FIELD_MAP = {
    "hotshot-block": ("streamer", "hotshot-block"),
    "espresso-txns-polling-interval": ("streamer", "txns-polling-interval"),
    "address-monitor-step": ("streamer", "address-monitor-step"),
    "address-monitor-start-l1": ("streamer", "address-monitor-start-l1"),

    "espresso-tee-type": ("batch-poster", "tee-type"),
    "espresso-register-service-config": ("batch-poster", "register-service-config"),
    "hotshot-url": ("batch-poster", "hotshot-url"),
    "espresso-txns-sending-interval": ("batch-poster", "txns-sending-interval"),
    "espresso-txns-resubmission-interval": ("batch-poster", "txns-resubmission-interval"),
    "resubmit-espresso-tx-deadline": ("batch-poster", "resubmit-espresso-tx-deadline"),
    "espresso-tx-size-limit": ("batch-poster", "tx-size-limit"),
    "user-data-attestation-file": ("batch-poster", "user-data-attestation-file"),
    "quote-file": ("batch-poster", "quote-file"),
    "attestation-service-url": ("batch-poster", "attestation-service-url"),
    "espresso-event-polling-step": ("batch-poster", "event-polling-step"),
    "hotshot-first-posting-block": ("batch-poster", "hotshot-first-posting-block"),
    "init-batcher-addresses": ("batch-poster", "init-batcher-addresses"),
    "address-valid-ranges": ("batch-poster", "address-valid-ranges"),
}

OLD_CAFF_NODE_KEY = "espresso-caff-node"
MIN_BLOCK_KEY = "minimum-hotshot-block-num"
ADDRESS_MONITOR_KEYS = ["address-monitor-step", "address-monitor-start-l1"]
REMOVE_KEYS = ["from-block"]

def migrate_config(cfg: dict) -> dict:
    if "node" not in cfg or not isinstance(cfg["node"], dict):
        return cfg

    old_node = cfg["node"]
    new_node = {}
    espresso = {}

    batch_poster = old_node.get("batch-poster", {})
    if isinstance(batch_poster, dict):
        remaining_batch_poster = {}
        for k, v in batch_poster.items():
            if k in ESPRESSO_FIELD_MAP:
                section, new_key = ESPRESSO_FIELD_MAP[k]
                espresso.setdefault(section, {})[new_key] = v
            else:
                remaining_batch_poster[k] = v
        

    if OLD_CAFF_NODE_KEY in old_node:
        old_caff = old_node.get(OLD_CAFF_NODE_KEY, {})
        if isinstance(old_caff, dict):
            caff_node = espresso.setdefault("caff-node", {})
            for k, v in old_caff.items():
                if k == "from-block":
                    if "streamer" not in espresso:
                        espresso["streamer"] = OrderedDict()
                    espresso["streamer"]["hotshot-block"] = v
                if k in REMOVE_KEYS:
                    continue
                if k in ADDRESS_MONITOR_KEYS:
                    espresso.setdefault("streamer", {})[k] = v
                elif k == "dangerous" and isinstance(v, dict):
                    streamer_dangerous = espresso.setdefault("streamer", {}).setdefault("dangerous", {})
                    if MIN_BLOCK_KEY in v:
                        streamer_dangerous[MIN_BLOCK_KEY] = v[MIN_BLOCK_KEY]

                    remaining = {kk: vv for kk, vv in v.items() if kk != MIN_BLOCK_KEY}
                    if remaining:
                        caff_node["dangerous"] = remaining
                else:
                    new_k = k.replace("espresso-", "") if k.startswith("espresso-") else k
                    caff_node[new_k] = v
            
            if not caff_node: espresso.pop("caff-node", None)

    for key, value in old_node.items():
        if key == "batch-poster":
            if remaining_batch_poster:
                new_node[key] = remaining_batch_poster
        elif key == OLD_CAFF_NODE_KEY:
            continue
        else:
            new_node[key] = value

    if espresso:
        new_node["espresso"] = espresso

    cfg["node"] = new_node
    return cfg

def main():
    if len(sys.argv) < 3:
        print("Usage: python3 migrate.py <input.json> <output.json>")
        sys.exit(1)

    input_file = sys.argv[1]
    output_file = sys.argv[2]

    try:
        with open(input_file, 'r') as f:
            cfg = json.load(f, object_pairs_hook=OrderedDict)

        new_cfg = migrate_config(cfg)

        with open(output_file, "w") as f:
            json.dump(new_cfg, f, indent=2, sort_keys=False)

        print(f"Migration successful: {output_file}")
    except Exception as e:
        print(f"Critical Error: {e}")

if __name__ == "__main__":
    main()