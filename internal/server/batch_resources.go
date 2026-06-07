package server

import "net/http"

type versionedBatchLookup struct {
	UserUUID        string
	KnownVersion    int
	HasKnownVersion bool
}

func collectBatchUserLookups(req batchUsersRequest) ([]versionedBatchLookup, *requestError) {
	if len(req.UserUUIDs)+len(req.Items) > maxBatchUsers {
		return nil, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "batch users must contain at most 100 entries",
		}
	}

	lookups := make([]versionedBatchLookup, 0, len(req.UserUUIDs)+len(req.Items))
	indexByID := map[string]int{}
	for _, userUUID := range req.UserUUIDs {
		if err := validateUUID(userUUID, "userUuids"); err != nil {
			return nil, err
		}
		lookups = mergeVersionedBatchLookup(lookups, indexByID, versionedBatchLookup{UserUUID: userUUID})
	}
	for _, item := range req.Items {
		if err := validateUUID(item.UserUUID, "items.userUuid"); err != nil {
			return nil, err
		}
		lookup := versionedBatchLookup{UserUUID: item.UserUUID}
		if item.KnownVersion != nil {
			if *item.KnownVersion < 0 {
				return nil, &requestError{
					Status:  http.StatusBadRequest,
					Code:    "VALIDATION_ERROR",
					Message: "items.knownVersion must be non-negative",
				}
			}
			lookup.KnownVersion = *item.KnownVersion
			lookup.HasKnownVersion = true
		}
		lookups = mergeVersionedBatchLookup(lookups, indexByID, lookup)
	}
	return lookups, nil
}

func collectBatchGroupMemberLookups(req batchGroupMembersRequest) ([]versionedBatchLookup, *requestError) {
	if len(req.UserUUIDs)+len(req.Items) > maxBatchUsers {
		return nil, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "batch group members must contain at most 100 entries",
		}
	}

	lookups := make([]versionedBatchLookup, 0, len(req.UserUUIDs)+len(req.Items))
	indexByID := map[string]int{}
	for _, userUUID := range req.UserUUIDs {
		if err := validateUUID(userUUID, "userUuids"); err != nil {
			return nil, err
		}
		lookups = mergeVersionedBatchLookup(lookups, indexByID, versionedBatchLookup{UserUUID: userUUID})
	}
	for _, item := range req.Items {
		if err := validateUUID(item.UserUUID, "items.userUuid"); err != nil {
			return nil, err
		}
		lookup := versionedBatchLookup{UserUUID: item.UserUUID}
		if item.KnownVersion != nil {
			if *item.KnownVersion < 0 {
				return nil, &requestError{
					Status:  http.StatusBadRequest,
					Code:    "VALIDATION_ERROR",
					Message: "items.knownVersion must be non-negative",
				}
			}
			lookup.KnownVersion = *item.KnownVersion
			lookup.HasKnownVersion = true
		}
		lookups = mergeVersionedBatchLookup(lookups, indexByID, lookup)
	}
	return lookups, nil
}

func mergeVersionedBatchLookup(lookups []versionedBatchLookup, indexByID map[string]int, lookup versionedBatchLookup) []versionedBatchLookup {
	if index, ok := indexByID[lookup.UserUUID]; ok {
		existing := &lookups[index]
		if !existing.HasKnownVersion || !lookup.HasKnownVersion {
			existing.HasKnownVersion = false
			existing.KnownVersion = 0
			return lookups
		}
		if lookup.KnownVersion < existing.KnownVersion {
			existing.KnownVersion = lookup.KnownVersion
		}
		return lookups
	}
	indexByID[lookup.UserUUID] = len(lookups)
	return append(lookups, lookup)
}
