package ldevents

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	helpers "github.com/launchdarkly/go-test-helpers"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

var optionalStringSetters = map[lduser.UserAttribute]func(lduser.UserBuilder, string) lduser.UserBuilderCanMakeAttributePrivate{
	lduser.SecondaryKeyAttribute: lduser.UserBuilder.Secondary,
	lduser.IPAttribute:           lduser.UserBuilder.IP,
	lduser.CountryAttribute:      lduser.UserBuilder.Country,
	lduser.EmailAttribute:        lduser.UserBuilder.Email,
	lduser.FirstNameAttribute:    lduser.UserBuilder.FirstName,
	lduser.LastNameAttribute:     lduser.UserBuilder.LastName,
	lduser.AvatarAttribute:       lduser.UserBuilder.Avatar,
	lduser.NameAttribute:         lduser.UserBuilder.Name,
}

const customAttrName1 = "thing1"
const customAttrName2 = "thing2"

var customAttrValue1 = ldvalue.String("value1")
var customAttrValue2 = ldvalue.String("value2")

func buildUserWithAllAttributes() lduser.UserBuilder {
	return lduser.NewUserBuilder("user-key").
		FirstName("sam").
		LastName("smith").
		Name("sammy").
		Country("freedonia").
		Avatar("my-avatar").
		IP("123.456.789").
		Email("me@example.com").
		Secondary("abcdef").
		Anonymous(true).
		Custom(customAttrName1, customAttrValue1).
		Custom(customAttrName2, customAttrValue2)
}

func getAllPrivatableAttributeNames() []string {
	ret := []string{customAttrName1, customAttrName2}
	for a := range optionalStringSetters {
		ret = append(ret, string(a))
	}
	sort.Strings(ret)
	return ret
}

func TestScrubUserWithNoFiltering(t *testing.T) {
	t.Run("user with no attributes", func(t *testing.T) {
		filter := newUserFilter(epDefaultConfig)
		u := EventUser{lduser.NewUser("user-key"), nil}
		fu := filter.scrubUser(u).filteredUser
		assert.Equal(t,
			filteredUser{
				Key: u.GetKey(),
			}, fu)
	})
	t.Run("user with all attributes", func(t *testing.T) {
		filter := newUserFilter(epDefaultConfig)
		u := EventUser{buildUserWithAllAttributes().Build(), nil}
		fu := filter.scrubUser(u).filteredUser
		assert.Equal(t,
			filteredUser{
				Key:       u.GetKey(),
				FirstName: u.GetFirstName().AsPointer(),
				Name:      u.GetName().AsPointer(),
				LastName:  u.GetLastName().AsPointer(),
				Country:   u.GetCountry().AsPointer(),
				Avatar:    u.GetAvatar().AsPointer(),
				IP:        u.GetIP().AsPointer(),
				Email:     u.GetEmail().AsPointer(),
				Secondary: u.GetSecondaryKey().AsPointer(),
				Custom:    u.GetAllCustom().AsPointer(),
				Anonymous: helpers.BoolPtr(true),
			}, fu)
	})
}

func TestScrubUserWithPerUserPrivateAttributes(t *testing.T) {
	filter := newUserFilter(epDefaultConfig)
	fu0 := filter.scrubUser(EventUser{buildUserWithAllAttributes().Build(), nil}).filteredUser
	for attr, setter := range optionalStringSetters {
		t.Run(string(attr), func(t *testing.T) {
			builder := buildUserWithAllAttributes()
			setter(builder, "private-value").AsPrivateAttribute()
			u1 := EventUser{builder.Build(), nil}
			fu1 := filter.scrubUser(u1).filteredUser
			assert.Equal(t, []string{string(attr)}, fu1.PrivateAttrs)
			fu1.PrivateAttrs = nil
			assert.NotEqual(t, fu0, fu1)
		})
	}
	t.Run("custom", func(t *testing.T) {
		u1 := buildUserWithAllAttributes().
			Custom(customAttrName1, customAttrValue1).AsPrivateAttribute().
			Build()
		fu1 := filter.scrubUser(EventUser{u1, nil}).filteredUser
		assert.Equal(t, []string{customAttrName1}, fu1.PrivateAttrs)
		assert.Equal(t, ldvalue.ObjectBuild().Set(customAttrName2, customAttrValue2).Build().AsPointer(),
			fu1.Custom)
		fu1.PrivateAttrs = nil
		assert.NotEqual(t, fu0, fu1)
		fu1.Custom = u1.GetAllCustom().AsPointer()
		assert.Equal(t, fu0, fu1)
	})
}

func TestScrubUserWithGlobalPrivateAttributes(t *testing.T) {
	filter0 := newUserFilter(epDefaultConfig)
	u := EventUser{buildUserWithAllAttributes().Build(), nil}
	fu0 := filter0.scrubUser(u).filteredUser
	for attr := range optionalStringSetters {
		t.Run(string(attr), func(t *testing.T) {
			config := epDefaultConfig
			config.PrivateAttributeNames = []lduser.UserAttribute{attr}
			filter1 := newUserFilter(config)
			fu1 := filter1.scrubUser(u).filteredUser
			assert.Equal(t, []string{string(attr)}, fu1.PrivateAttrs)
			fu1.PrivateAttrs = nil
			assert.NotEqual(t, fu0, fu1)
		})
	}
	t.Run("custom", func(t *testing.T) {
		config := epDefaultConfig
		config.PrivateAttributeNames = []lduser.UserAttribute{lduser.UserAttribute(customAttrName1)}
		filter1 := newUserFilter(config)
		fu1 := filter1.scrubUser(u).filteredUser
		assert.Equal(t, []string{customAttrName1}, fu1.PrivateAttrs)
		assert.Equal(t, ldvalue.ObjectBuild().Set(customAttrName2, customAttrValue2).Build().AsPointer(),
			fu1.Custom)
		fu1.PrivateAttrs = nil
		assert.NotEqual(t, fu0, fu1)
		fu1.Custom = u.GetAllCustom().AsPointer()
		assert.Equal(t, fu0, fu1)
	})
	t.Run("allAttributesPrivate", func(t *testing.T) {
		config := epDefaultConfig
		config.AllAttributesPrivate = true
		filter1 := newUserFilter(config)
		fu1 := filter1.scrubUser(u).filteredUser
		sort.Strings(fu1.PrivateAttrs)
		assert.Equal(t,
			filteredUser{
				Key:          u.GetKey(),
				Anonymous:    helpers.BoolPtr(true),
				PrivateAttrs: getAllPrivatableAttributeNames(),
			}, fu1)
	})
}

func TestPrefilteredAttributesAreUsedUnchanged(t *testing.T) {
	config := epDefaultConfig
	config.AllAttributesPrivate = true // this should be ignored
	user := lduser.NewUserBuilder("user-key").Name("me").Build()
	eventUser := EventUser{User: user, AlreadyFilteredAttributes: []string{"firstName"}}
	filter := newUserFilter(config)
	fu := filter.scrubUser(eventUser).filteredUser
	assert.Equal(t,
		filteredUser{
			Key:          user.GetKey(),
			Name:         user.GetName().AsPointer(),
			PrivateAttrs: []string{"firstName"},
		}, fu)
}

func TestEmptyListOfPrefilteredAttributesIsUsedUnchanged(t *testing.T) {
	// This tests that setting AlreadyFilteredAttributes to an empty slice, unlike leaving it nil, is treated as
	// a sign that the user was already filtered and did not have any private attributes.
	config := epDefaultConfig
	config.AllAttributesPrivate = true // this should be ignored
	user := lduser.NewUserBuilder("user-key").Name("me").Build()
	eventUser := EventUser{User: user, AlreadyFilteredAttributes: []string{}}
	filter := newUserFilter(config)
	fu := filter.scrubUser(eventUser).filteredUser
	assert.Equal(t,
		filteredUser{
			Key:          user.GetKey(),
			Name:         user.GetName().AsPointer(),
			PrivateAttrs: []string{},
		}, fu)
}
