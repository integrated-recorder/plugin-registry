## Registry approval review

- [ ] Plugin ID, type, and repository identity are correct.
- [ ] The release exists and the immutable source commit is a full 40-hex SHA.
- [ ] Protocol family/version and executable descriptor match the plugin type and version.
- [ ] Artifact URLs, filenames, platforms, byte sizes, and SHA-256 values are exact.
- [ ] Requested network/filesystem authority and secret handling were reviewed.
- [ ] License and unexpected dependencies were reviewed.
- [ ] Existing approved releases remain unchanged; only a new release or channel move is proposed.
- [ ] No fixture/demo entry is being added to the production catalog.

Describe the release and reviewer evidence. CI verifies the artifact and descriptor, but does not
build plugin source or prove that a plugin is safe.
