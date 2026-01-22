#!/usr/bin/env python3
import json
import sys
from collections import OrderedDict


# -------------------- Constants --------------------

ESPRESSO_FIELD_MAP = {
    # streamer
    "hotshot-block": ("streamer", "hotshot-block"),
    "espresso-txns-polling-interval": ("streamer", "txns-polling-interval"),
    "address-monitor-step": ("streamer", "address-monitor-step"),
    "address-monitor-start-l1": ("streamer", "address-monitor-start-l1"),

    # batch-poster
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

ADDRESS_MONITOR_KEYS = [
    "address-monitor-step",
    "address-monitor-start-l1",
]

REMOVE_KEYS = [
    "from-block",
    "retry-time",
]


# -------------------- Migration Helpers --------------------

def migrate_batch_poster(old_node: dict, espresso: dict) -> dict:
    """
    Extract espresso-related batch-poster fields into espresso sections.
    Return remaining batch-poster fields.
    """
    remaining = {}
    batch_poster = old_node.get("batch-poster", {})

    if not isinstance(batch_poster, dict):
        return remaining

    for key, value in batch_poster.items():
        if key in ESPRESSO_FIELD_MAP:
            section, new_key = ESPRESSO_FIELD_MAP[key]
            espresso.setdefault(section, {})[new_key] = value
        else:
            remaining[key] = value

    return remaining


def migrate_caff_node(old_node: dict, espresso: dict) -> None:
    """
    Migrate espresso-caff-node into espresso.{streamer,caff-node}.
    Mutates espresso in-place.
    """
    old_caff = old_node.get(OLD_CAFF_NODE_KEY)

    if not isinstance(old_caff, dict):
        return

    caff_node = espresso.setdefault("caff-node", {})

    for key, value in old_caff.items():
        # from-block → streamer.hotshot-block
        if key == "from-block":
            espresso.setdefault("streamer", OrderedDict())["hotshot-block"] = value

        if key == "retry-time":
            espresso.setdefault("streamer", OrderedDict())["txns-polling-interval"] = value

        if key in REMOVE_KEYS:
            continue

        # address monitor keys → streamer
        if key in ADDRESS_MONITOR_KEYS:
            espresso.setdefault("streamer", {})[key] = value
            continue

        # dangerous handling
        if key == "dangerous" and isinstance(value, dict):
            migrate_dangerous_block(value, espresso, caff_node)
            continue

        # default: strip espresso- prefix
        new_key = key.replace("espresso-", "") if key.startswith("espresso-") else key
        caff_node[new_key] = value

    if not caff_node:
        espresso.pop("caff-node", None)


def migrate_dangerous_block(
    dangerous: dict,
    espresso: dict,
    caff_node: dict,
) -> None:
    """
    Split dangerous fields between streamer and caff-node.
    """
    streamer_dangerous = (
        espresso
        .setdefault("streamer", {})
        .setdefault("dangerous", {})
    )

    if MIN_BLOCK_KEY in dangerous:
        streamer_dangerous[MIN_BLOCK_KEY] = dangerous[MIN_BLOCK_KEY]

    remaining = {
        k: v for k, v in dangerous.items()
        if k != MIN_BLOCK_KEY
    }

    if remaining:
        caff_node["dangerous"] = remaining


def rebuild_node(
    old_node: dict,
    remaining_batch_poster: dict,
    espresso: dict,
) -> dict:
    """
    Rebuild node with migrated espresso config and preserved fields.
    """
    new_node = {}

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

    return new_node


# -------------------- Top-Level Migration --------------------

def migrate_config(cfg: dict) -> dict:
    if "node" not in cfg or not isinstance(cfg["node"], dict):
        return cfg

    old_node = cfg["node"]
    espresso = {}

    remaining_batch_poster = migrate_batch_poster(old_node, espresso)
    migrate_caff_node(old_node, espresso)

    cfg["node"] = rebuild_node(
        old_node,
        remaining_batch_poster,
        espresso,
    )

    return cfg


# -------------------- CLI --------------------

def main():
    if len(sys.argv) < 3:
        print("Usage: python3 migrate.py <input.json> <output.json>")
        sys.exit(1)

    input_file = sys.argv[1]
    output_file = sys.argv[2]

    try:
        with open(input_file, "r") as f:
            cfg = json.load(f, object_pairs_hook=OrderedDict)

        new_cfg = migrate_config(cfg)

        with open(output_file, "w") as f:
            json.dump(new_cfg, f, indent=2, sort_keys=False)

        print(f"Migration successful: {output_file}")

    except Exception as e:
        print(f"Critical Error: {e}")


if __name__ == "__main__":
    main()
