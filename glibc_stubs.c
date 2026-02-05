// Stub implementations for glibc functions that Rust's stdlib references
// but aren't available in musl. These functions are in code paths that
// won't be executed on musl systems, but the symbols need to exist for linking.

const char* gnu_get_libc_version(void) {
    // Return a fake version - this code path should never be taken on musl
    return "stub";
}

int __res_init(void) {
    // Return success - this code path should never be taken on musl
    return 0;
}
