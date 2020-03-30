package service

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/sipcapture/homer-app/auth"
	"github.com/sipcapture/homer-app/model"
	"github.com/sipcapture/homer-app/utils/heputils"
	"github.com/sipcapture/homer-app/utils/ldap"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	ServiceConfig
	LdapClient *ldap.LDAPClient
}

// this method gets all users from database
func (us *UserService) GetUser(UserName string, isAdmin bool) ([]*model.TableUser, int, error) {

	var user []*model.TableUser
	var sqlWhere = make(map[string]interface{})

	if !isAdmin {
		sqlWhere = map[string]interface{}{"username": UserName}
	}

	if err := us.Session.Debug().Table("users").Where(sqlWhere).Find(&user).Error; err != nil {
		return user, 0, err
	}

	return user, len(user), nil
}

// this method create new user in the database
// it doesn't check internally whether all the validation are applied or not
func (us *UserService) CreateNewUser(user *model.TableUser) error {

	user.CreatedAt = time.Now()

	if user.Password == "" {
		return errors.New("empty password")
	}

	// lets generate hash from password
	password := []byte(user.Password)

	// Hashing the password with the default cost of 10
	hashedPassword, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	user.Hash = string(hashedPassword)

	err = us.Session.Debug().Table("users").Create(&user).Error
	if err != nil {
		return err
	}
	return nil
}

// this method update user info in the database
// it doesn't check internally whether all the validation are applied or not
func (us *UserService) UpdateUser(user *model.TableUser, UserName string, isAdmin bool) error {

	// get new instance of user data source
	user.CreatedAt = time.Now()
	oldRecord := model.TableUser{}

	var sqlWhere = make(map[string]interface{})

	if !isAdmin {
		sqlWhere = map[string]interface{}{"guid": user.GUID, "username": UserName}
	} else {
		sqlWhere = map[string]interface{}{"guid": user.GUID}
	}

	if us.Session.Where(sqlWhere).Find(&oldRecord).RecordNotFound() {
		return errors.New(fmt.Sprintf("the user with id '%s' was not found", user.GUID))
	}

	if user.Password != "" {
		password := []byte(user.Password)
		// Hashing the password with the default cost of 10
		hashedPassword, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		user.Hash = string(hashedPassword)
	} else {
		user.Hash = oldRecord.Hash
	}
	if !isAdmin {
		err := us.Session.Debug().Table("users").Model(&model.TableUser{}).Where(sqlWhere).Update(model.TableUser{Email: user.Email, FirstName: user.FirstName, LastName: user.LastName,
			Department: user.Department, Hash: user.Hash, CreatedAt: user.CreatedAt}).Error
		if err != nil {
			return err
		}
	} else {
		err := us.Session.Debug().Table("users").Model(&model.TableUser{}).Where(sqlWhere).Update(model.TableUser{UserName: user.UserName,
			PartId: user.PartId, Email: user.Email, FirstName: user.FirstName, LastName: user.LastName, Department: user.Department, UserGroup: user.UserGroup,
			Hash: user.Hash, CreatedAt: user.CreatedAt}).Error
		if err != nil {
			return err
		}
	}

	return nil

}

// this method deletes user in the database
// it doesn't check internally whether all the validation are applied or not
func (us *UserService) DeleteUser(user *model.TableUser) error {

	// get new instance of user data source
	newUser := model.TableUser{}

	if us.Session.Where("guid =?", user.GUID).Find(&newUser).RecordNotFound() {
		return errors.New(fmt.Sprintf("the user with id '%s' was not found", user.GUID))
	}
	err := us.Session.Debug().Where("guid =?", user.GUID).Delete(&model.TableUser{}).Error
	if err != nil {
		return err
	}
	return nil
}

// this method is used to login the user
// it doesn't check internally whether all the validation are applied or not
func (us *UserService) LoginUser(username, password string) (string, model.TableUser, error) {
	userData := model.TableUser{}

	if us.LdapClient != nil {
		ok, isAdmin, user, err := us.LdapClient.Authenticate(username, password)
		if err != nil {
			errorString := fmt.Sprintf("Error authenticating user %s: %+v", username, err)
			return "", userData, errors.New(errorString)
		}

		if !ok {
			return "", userData, errors.New("Authenticating failed for user")
		}

		userData.UserName = username
		userData.Id = int(hashString(username))
		userData.Password = password
		userData.FirstName = username
		userData.IsAdmin = isAdmin
		userData.ExternalAuth = true
		if val, ok := user["dn"]; ok {
			userData.UserGroup = val
		}

		groups, err := us.LdapClient.GetGroupsOfUser(user["dn"])
		if err != nil {
			logrus.Error("Couldn't get any group for user: ", username)
		} else {
			if heputils.ElementExists(groups, us.LdapClient.AdminGroup) {
				userData.IsAdmin = true
			}
		}

	} else {
		if err := us.Session.Debug().Table("users").Where("username =?", username).Find(&userData).Error; err != nil {
			return "", userData, errors.New("user is not found")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(userData.Hash), []byte(password)); err != nil {
			return "", userData, errors.New("password is not correct")
		}

		/* check admin or not */
		userData.IsAdmin = false
		userData.ExternalAuth = false
		if userData.UserGroup != "" && strings.Contains(strings.ToLower(userData.UserGroup), "admin") {
			userData.IsAdmin = true
		}

	}

	token, err := auth.Token(userData)
	return token, userData, err
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
