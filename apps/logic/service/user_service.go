package service

import (
	"errors"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/proto/pb_user"
	"go-im-system/apps/pkg/utils"
)

func (s *LogicService) UserLogin(args *pb_user.UserLoginArgs, reply *pb_user.UserLoginReply) error {
	user, err := dao.FindUserByName(args.UserName)
	if err != nil {
		return errors.New("用户不存在")
	}

	if !utils.CheckPasswordHash(args.Password, user.Password) {
		return errors.New("密码错误")
	} else {
		reply.UserId = user.UserId
		reply.Token, err = utils.GenerateToken(user.UserId)
		if err != nil {
			return errors.New("token生成失败")
		}
	}
	return nil
}

func (s *LogicService) UserRegister(args *pb_user.UserRegisterArgs, reply *pb_user.UserRegisterReply) error {
	password, err := utils.HashPassword(args.Password)
	if err != nil {
		return err
	}

	// 缺省头像兜底
	finalAvatar := args.Avatar
	user := models.NewUser(args.UserId, args.UserName, password, args.Nickname, finalAvatar)
	err = dao.InsertUser(user)
	if err != nil {
		return err
	}
	reply.Success = true
	return nil
}
