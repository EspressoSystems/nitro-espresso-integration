#!/usr/bin/env python3
import json
import copy
import sys
from pathlib import Path
    # old_key: (espresso_section, new_key)
ESPRESSO_FIELD_MAP = {
    "hotshot-block": ("streamer", "hotshot-block"),

    # [espresso][batch-poster]
    "espresso-tee-type": ("batch-poster", "espresso-tee-type"),
    "espresso-register-service-config": ("batch-poster", "espresso-register-service-config"),
    "hotshot-url": ("batch-poster", "hotshot-url"),
    "espresso-txns-polling-interval": ("batch-poster", "espresso-txns-polling-interval"),
    "espresso-txns-sending-interval": ("batch-poster", "espresso-txns-sending-interval"),
    "espresso-txns-resubmission-interval": ("batch-poster", "espresso-txns-resubmission-interval"),
    "resubmit-espresso-tx-deadline": ("batch-poster", "resubmit-espresso-tx-deadline"),
    "espresso-tx-size-limit": ("batch-poster", "espresso-tx-size-limit"),
    "user-data-attestation-file": ("batch-poster", "user-data-attestation-file"),
    "quote-file": ("batch-poster", "quote-file"),
    "attestation-service-url": ("batch-poster", "attestation-service-url"),
    "espresso-event-polling-step": ("batch-poster", "espresso-event-polling-step"),
    "hotshot-first-posting-block": ("batch-poster", "hotshot-first-posting-block"),
    "address-monitor-start-l1": ("batch-poster", "address-monitor-start-l1"),
    "init-batcher-addresses": ("batch-poster", "init-batcher-addresses"),
    "address-monitor-step": ("batch-poster", "address-monitor-step"),
    "address-valid-ranges": ("batch-poster", "address-valid-ranges"),

}

OLD_CAFF_NODE_KEY = "espresso-caff-node"
NEW_CAFF_NODE_PATH = ("espresso", "caff-node")
STREAMER_KEY = "streamer"
DANGEROUS_KEY = "dangerous"
MIN_BLOCK_KEY = "minimum-hotshot-block-num"

def migrate_config(cfg: dict) -> dict:
    cfg = copy.deepcopy(cfg)

    node = cfg.get("node")
    if not node:
        return cfg

    espresso = cfg.setdefault("espresso", {})

    # ---- Migrate batch-poster ----
    batch_poster = node.get("batch-poster")
    if not batch_poster:
        return cfg


    for old_key, (section, new_key) in ESPRESSO_FIELD_MAP.items():
        if old_key in batch_poster:
            section_cfg = espresso.setdefault(section, {})
            section_cfg.setdefault(new_key, batch_poster[old_key])
            batch_poster.pop(old_key, None)

    if not batch_poster:
        node.pop("batch-poster", None)


    # ---- Migrate caff-node  ----
    if OLD_CAFF_NODE_KEY in node:
        espresso.setdefault("caff-node", node[OLD_CAFF_NODE_KEY])
        node.pop(OLD_CAFF_NODE_KEY, None)
    
    caff = espresso.get("caff-node")
    if not caff:
        return

    dangerous = caff.get("dangerous")
    if not dangerous:
        return

    if MIN_BLOCK_KEY not in dangerous:
        return

    #  ---- Migrate streamer  ----
    streamer = espresso.setdefault("streamer", {})
    streamer_dangerous = streamer.setdefault("dangerous", {})

    streamer_dangerous.setdefault(
        MIN_BLOCK_KEY,
        dangerous[MIN_BLOCK_KEY],
    )

    dangerous.pop(MIN_BLOCK_KEY, None)

    if not dangerous:
        caff.pop("dangerous", None)

    if not node:
        cfg.pop("node", None)

    return cfg



if len(sys.argv) < 3:
    print("Usage: python3 migrate-config-file.py <old_config.json> <new_config.json>")
    sys.exit(1)

old_config = sys.argv[1]
new_config = sys.argv[2]

with open(old_config, 'r') as f:
    cfg = json.load(f)

new_cfg = migrate_config(cfg)

with open(new_config, "w") as f:
    json.dump(new_cfg, f, indent=2, sort_keys=True)

print(f"Converted config written to {new_config}")
