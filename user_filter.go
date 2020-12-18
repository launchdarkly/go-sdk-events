package ldevents

import (
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldlog"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

type userFilter struct {
	allAttributesPrivate    bool
	globalPrivateAttributes []lduser.UserAttribute
	loggers                 ldlog.Loggers
	logUserKeyInErrors      bool
}

func newUserFilter(config EventsConfiguration) userFilter {
	return userFilter{
		allAttributesPrivate:    config.AllAttributesPrivate,
		globalPrivateAttributes: config.PrivateAttributeNames,
		loggers:                 config.Loggers,
		logUserKeyInErrors:      config.LogUserKeyInErrors,
	}
}

// Produces a JSON serialization of the user for event data and writes it to the specified JSONBuffer.
//
// If neither the configuration nor the user specifies any private attributes, then this is the same
// as the original user. Otherwise, it is a copy which may have some attributes removed (with the
// PrivateAttributes property set to a list of their names).
func (uf *userFilter) writeUser(w *jwriter.Writer, user EventUser) {
	obj := w.Object()

	obj.Name("key").String(user.GetKey())

	var privateAttrsPreallocateArray [30]string // lets us avoid heap allocation in typical cases
	var privateAttrs = privateAttrsPreallocateArray[0:0]

	uf.maybeStringAttribute(&obj, &user, lduser.SecondaryKeyAttribute, user.GetSecondaryKey(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.IPAttribute, user.GetIP(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.CountryAttribute, user.GetCountry(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.EmailAttribute, user.GetEmail(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.LastNameAttribute, user.GetLastName(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.FirstNameAttribute, user.GetFirstName(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.AvatarAttribute, user.GetAvatar(), &privateAttrs)
	uf.maybeStringAttribute(&obj, &user, lduser.NameAttribute, user.GetName(), &privateAttrs)

	if anon, hasAnon := user.GetAnonymousOptional(); hasAnon {
		obj.Name(string(lduser.AnonymousAttribute)).Bool(anon)
	}

	custom := user.GetAllCustom()
	wroteCustom := false
	var customObj jwriter.ObjectState
	if custom.Count() > 0 {
		custom.Enumerate(func(i int, key string, value ldvalue.Value) bool {
			if uf.isPrivate(&user, lduser.UserAttribute(key)) {
				privateAttrs = append(privateAttrs, key)
			} else {
				if !wroteCustom {
					customObj = obj.Name("custom").Object()
					wroteCustom = true
				}
				value.WriteToJSONWriter(customObj.Name(key))
			}
			return true
		})
	}
	if wroteCustom {
		customObj.End()
	}

	if user.AlreadyFilteredAttributes != nil {
		privateAttrs = user.AlreadyFilteredAttributes
	}
	if len(privateAttrs) > 0 {
		attrsArr := obj.Name("privateAttrs").Array()
		for _, a := range privateAttrs {
			attrsArr.String(a)
		}
		attrsArr.End()
	}

	obj.End()
}

func (uf *userFilter) maybeStringAttribute(
	obj *jwriter.ObjectState,
	user *EventUser,
	attrName lduser.UserAttribute,
	value ldvalue.OptionalString,
	privateAttrs *[]string) {
	if s, ok := value.Get(); ok {
		if uf.isPrivate(user, attrName) {
			*privateAttrs = append(*privateAttrs, string(attrName))
		} else {
			obj.Name(string(attrName)).String(s)
		}
	}
}

func (uf *userFilter) isPrivate(user *EventUser, attrName lduser.UserAttribute) bool {
	// If alreadyFiltered is true, it means this is user data that has already gone through the
	// attribute filtering logic, so the private attribute values have already been removed and their
	// names are in user.AlreadyFilteredAttributes. This happens when Relay receives event data from
	// the PHP SDK. In this case, we do not need to repeat the filtering logic and we do not support
	// re-filtering with a different private attribute configuration.
	if user.AlreadyFilteredAttributes != nil {
		return false
	}
	if uf.allAttributesPrivate || user.IsPrivateAttribute(attrName) {
		return true
	}
	for _, a := range uf.globalPrivateAttributes {
		if a == attrName {
			return true
		}
	}
	return false
}
