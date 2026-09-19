package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// runResetPassword lists all administrators and interactively resets the password of the selected one.
func runResetPassword(ctx context.Context, store *Store, in io.Reader, out io.Writer) error {
	admins, err := store.ListAdmins(ctx)
	if err != nil {
		return fmt.Errorf("list administrators: %w", err)
	}
	if len(admins) == 0 {
		fmt.Fprintln(out, "尚未初始化管理员账户，请启动服务后在网页中完成初始化。")
		return nil
	}

	fmt.Fprintln(out, "管理员账户：")
	for i, admin := range admins {
		fmt.Fprintf(out, "  [%d] %s（创建于 %s，更新于 %s）\n", i+1, admin.Username,
			admin.CreatedAt.Format("2006-01-02 15:04:05"), admin.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	reader := bufio.NewReader(in)
	selected := admins[0]
	if len(admins) > 1 {
		line, err := readLine(reader, out, fmt.Sprintf("请选择要重置密码的账户 [1-%d]: ", len(admins)))
		if err != nil {
			return err
		}
		index, err := strconv.Atoi(line)
		if err != nil || index < 1 || index > len(admins) {
			return fmt.Errorf("无效的选择：%q", line)
		}
		selected = admins[index-1]
	}
	fmt.Fprintf(out, "将重置账户 %s 的密码。\n", selected.Username)

	password, err := readPassword(reader, in, out, "新密码: ")
	if err != nil {
		return err
	}
	if err := validateCredentials(selected.Username, password); err != nil {
		return err
	}
	confirmation, err := readPassword(reader, in, out, "确认新密码: ")
	if err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("两次输入的密码不一致")
	}

	if err := store.ResetAdminPassword(ctx, selected.ID, password); err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	fmt.Fprintf(out, "账户 %s 的密码已重置，原有登录会话已全部失效。\n", selected.Username)
	return nil
}

func readLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// readPassword hides the input when reading from a terminal and falls back to plain line input otherwise.
func readPassword(reader *bufio.Reader, in io.Reader, out io.Writer, prompt string) (string, error) {
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(out, prompt)
		password, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(password), nil
	}
	fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
