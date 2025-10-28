# Release Management Charter

## Mission

Deliver new code features systematically with clear documentation and validation before production release.

## Release Types

- **Pre-release**: New features ready for testing (on-going devnet testing)
- **Release**: Production-ready features (see [Release Criteria Section](#5-release-criteria)).

## Release Process

### 1. Feature Development

- [ ] Write design documentation on feature
- [ ] Build the feature ensuring it works with the upstream branch
- [ ] Add a feature enabling flag that allows to run the code without the feature being activated
- [ ] Merge code relatively regularly to avoid diverging too much

### 2. Pre-release Preparation

- **Feature Description**
  - [ ] Explain what the feature does in simple terms
  - [ ] Document why we built it and what problem it solves (can link to existing)
  - [ ] Maintain the changelogs.md file (`https://keepachangelog.com/en/1.1.0/`)

- **Chain Network Setup**
  - [ ] Update node configuration requirements (e.g., get hotshot block)
  - [ ] List any new dependencies (e.g., DA providers)

### 3. Pre-release Deployment

- [ ] Create the GitHub release and check "Set as a pre-release"
- [ ] Add a clear *warning* that this release is not production ready
- [ ] Add documentation from previous section

### 4. Pre-release Testing

- [ ] Create image from tag and deploy to devnet environment
- [ ] Enable Monitoring (e.g., alerts)
- [ ] Enable Load Testing
- [ ] Watch for stability issues and performance problems
- [ ] Collect feedback and document encountered issues/limitations

### 5. Release Criteria

**Pre-release becomes Release when:**

- [ ] Regression testing on new release went through successfully
- [ ] Managed to produced high load on the devnet for:
  - [ ] Tier 1 - 3 days successfully
  - [ ] Tier 2 - 1 week successfully
- [ ] Latest documentation updated
  - [ ] Update release documentation if needed
  - [ ] Describe tier'ed devnet testing results
  - [ ] Include security assessment if we conducted an audit
- [ ] Team agrees to making it production ready
